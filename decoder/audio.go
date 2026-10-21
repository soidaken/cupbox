package decoder

/*
#cgo CFLAGS: -I${SRCDIR}/../.tools/ffmpeg-min/include
#cgo LDFLAGS: -L${SRCDIR}/../.tools/ffmpeg-min/lib -lavformat -lavcodec -lavutil -lswscale -lswresample
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libswresample/swresample.h>
#include <libavutil/opt.h>
#include <stdlib.h>
#include <string.h>

// Audio comes out at this rate and channel count, always: the same as the
// system's output, so nothing resamples it again.
#define AD_RATE 48000
#define AD_CHANNELS 2

typedef struct {
    AVFormatContext *fc;
    AVCodecContext  *cc;
    struct SwrContext *swr;
    AVPacket *pkt;
    AVFrame  *frm;
    int stream;
    int draining;
    float *buf;      // converted, interleaved, waiting to be read
    int capFrames;   // buf's capacity in frames
    int bufFrames;   // frames in buf
    int bufPos;      // frames of buf already read
    int64_t durationMs;
} ad_t;

static void ad_close(ad_t *a) {
    if (a->swr) swr_free(&a->swr);
    if (a->frm) av_frame_free(&a->frm);
    if (a->pkt) av_packet_free(&a->pkt);
    if (a->cc) avcodec_free_context(&a->cc);
    if (a->fc) avformat_close_input(&a->fc);
    free(a->buf);
    free(a);
}

static int ad_open(const char *path, ad_t **out, char *err, int errlen) {
    av_log_set_level(AV_LOG_ERROR);
    ad_t *a = calloc(1, sizeof(ad_t));
    if (!a) return -1;
    a->capFrames = 16384;
    a->buf = malloc(sizeof(float) * AD_CHANNELS * a->capFrames);
    int e = avformat_open_input(&a->fc, path, NULL, NULL);
    if (e < 0) { av_strerror(e, err, errlen); ad_close(a); return -1; }
    // Avoid a second video/audio probe when MP4/MOV headers are complete.
    int headerAudio = av_find_best_stream(a->fc, AVMEDIA_TYPE_AUDIO, -1, -1, NULL, 0);
    int complete = headerAudio >= 0 && strstr(a->fc->iformat->name, "mov") &&
        a->fc->streams[headerAudio]->codecpar->sample_rate > 0 &&
        a->fc->streams[headerAudio]->codecpar->ch_layout.nb_channels > 0 &&
        a->fc->streams[headerAudio]->codecpar->codec_id != AV_CODEC_ID_NONE;
    if (!complete) {
        e = avformat_find_stream_info(a->fc, NULL);
        if (e < 0) { av_strerror(e, err, errlen); ad_close(a); return -1; }
    }
    a->stream = av_find_best_stream(a->fc, AVMEDIA_TYPE_AUDIO, -1, -1, NULL, 0);
    if (a->stream < 0) { snprintf(err, errlen, "no audio stream"); ad_close(a); return -1; }
    AVStream *st = a->fc->streams[a->stream];
    const AVCodec *dec = avcodec_find_decoder(st->codecpar->codec_id);
    if (!dec) { snprintf(err, errlen, "no audio decoder"); ad_close(a); return -1; }
    a->cc = avcodec_alloc_context3(dec);
    if (!a->cc) { snprintf(err, errlen, "no memory"); ad_close(a); return -1; }
    e = avcodec_parameters_to_context(a->cc, st->codecpar);
    if (e < 0) { av_strerror(e, err, errlen); ad_close(a); return -1; }
    e = avcodec_open2(a->cc, dec, NULL);
    if (e < 0) { av_strerror(e, err, errlen); ad_close(a); return -1; }
    if (a->fc->duration > 0) a->durationMs = a->fc->duration / (AV_TIME_BASE / 1000);
    if (st->duration > 0) a->durationMs = av_rescale_q(st->duration, st->time_base, (AVRational){1, 1000});
    a->pkt = av_packet_alloc();
    a->frm = av_frame_alloc();
    if (!a->pkt || !a->frm) { snprintf(err, errlen, "no memory"); ad_close(a); return -1; }
    *out = a;
    return 0;
}

static int ad_ensure_swr(ad_t *a, AVFrame *frm) {
    if (a->swr) return 0;
    AVChannelLayout out;
    av_channel_layout_default(&out, AD_CHANNELS);
    AVChannelLayout in = frm->ch_layout;
    if (in.nb_channels == 0) {
        int n = a->cc->ch_layout.nb_channels ? a->cc->ch_layout.nb_channels : 2;
        av_channel_layout_default(&in, n);
    }
    int e = swr_alloc_set_opts2(&a->swr, &out, AV_SAMPLE_FMT_FLT, AD_RATE,
        &in, frm->format, frm->sample_rate, 0, NULL);
    if (e < 0) return e;
    return swr_init(a->swr);
}

// ad_fill makes buf hold unconsumed frames. 0 = filled or nothing left,
// AVERROR_EOF = the stream ended, other = error.
static int ad_fill(ad_t *a) {
    while (a->bufPos >= a->bufFrames) {
        a->bufPos = a->bufFrames = 0;
        for (;;) {
            if (!a->draining) {
                int r = av_read_frame(a->fc, a->pkt);
                if (r < 0) {
                    avcodec_send_packet(a->cc, NULL);
                    a->draining = 1;
                } else {
                    if (a->pkt->stream_index == a->stream) {
                        int s = avcodec_send_packet(a->cc, a->pkt);
                        av_packet_unref(a->pkt);
                        if (s < 0 && s != AVERROR(EAGAIN)) return s;
                    } else {
                        av_packet_unref(a->pkt);
                    }
                }
            }
            int r = avcodec_receive_frame(a->cc, a->frm);
            if (r == AVERROR(EAGAIN)) {
                if (a->draining) return AVERROR_EOF;
                continue;
            }
            if (r == AVERROR_EOF) return AVERROR_EOF;
            if (r < 0) return r;
            if (ad_ensure_swr(a, a->frm) < 0) { av_frame_unref(a->frm); return -1; }
            int need = swr_get_out_samples(a->swr, a->frm->nb_samples);
            if (need > a->capFrames) {
                float *nb = realloc(a->buf, sizeof(float) * AD_CHANNELS * need);
                if (!nb) { av_frame_unref(a->frm); return AVERROR(ENOMEM); }
                a->buf = nb;
                a->capFrames = need;
            }
            int got = swr_convert(a->swr, (uint8_t **)&a->buf, a->capFrames,
                (const uint8_t **)a->frm->extended_data, a->frm->nb_samples);
            av_frame_unref(a->frm);
            if (got < 0) return got;
            a->bufFrames = got;
            if (got > 0) return 0;
        }
    }
    return 0;
}

// ad_read copies up to wantFrames frames of interleaved float32 into dst.
// It returns the frames written: 0 at the end of the stream, <0 on error.
static int ad_read(ad_t *a, float *dst, int wantFrames) {
    int written = 0;
    while (written < wantFrames) {
        int r = ad_fill(a);
        if (r == AVERROR_EOF) break;
        if (r < 0) return r;
        int avail = a->bufFrames - a->bufPos;
        if (avail <= 0) break;
        int n = avail < wantFrames - written ? avail : wantFrames - written;
        memcpy(dst + written * AD_CHANNELS, a->buf + a->bufPos * AD_CHANNELS,
            (size_t)n * AD_CHANNELS * sizeof(float));
        a->bufPos += n;
        written += n;
    }
    return written;
}

static int ad_seek(ad_t *a, int64_t ms) {
    AVStream *st = a->fc->streams[a->stream];
    int64_t ts = av_rescale_q(ms, (AVRational){1, 1000}, st->time_base);
    int e = av_seek_frame(a->fc, a->stream, ts, AVSEEK_FLAG_BACKWARD);
    if (e < 0) return e;
    avcodec_flush_buffers(a->cc);
    if (a->swr) swr_free(&a->swr);
    a->draining = 0;
    a->bufFrames = a->bufPos = 0;
    return 0;
}
*/
import "C"

import (
	"fmt"
	"io"
	"time"
	"unsafe"
)

// SampleRate is the rate every Audio stream decodes at.
const SampleRate = 48000

// Audio decodes the audio track of a file at 48 kHz stereo, interleaved
// float32, as the system's output wants it. It implements the audio
// decoder a mygo audio.Player plays.
type Audio struct {
	c      *C.ad_t
	dur    time.Duration
	frames int64 // duration in frames
}

// OpenAudio opens the audio track of path.
func OpenAudio(path string) (*Audio, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var c *C.ad_t
	var errbuf [256]C.char
	if rc := C.ad_open(cpath, &c, &errbuf[0], C.int(len(errbuf))); rc < 0 {
		return nil, fmt.Errorf("decoder: %s", C.GoString(&errbuf[0]))
	}
	a := &Audio{c: c}
	a.dur = time.Duration(c.durationMs) * time.Millisecond
	a.frames = int64(a.dur.Seconds() * SampleRate)
	return a, nil
}

// SampleRate returns the frames per second.
func (a *Audio) SampleRate() int { return SampleRate }

// Channels returns the samples per frame.
func (a *Audio) Channels() int { return 2 }

// Read decodes the next samples into buf, a whole number of frames, and
// returns how many it wrote, with io.EOF at the end.
func (a *Audio) Read(buf []float32) (int, error) {
	if len(buf) < 2 {
		return 0, nil
	}
	frames := len(buf) / 2
	n := int(C.ad_read(a.c, (*C.float)(unsafe.Pointer(&buf[0])), C.int(frames)))
	if n < 0 {
		return 0, fmt.Errorf("decoder: audio read failed (%d)", n)
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n * 2, nil
}

// SetPosition moves to frame, counted from the start.
func (a *Audio) SetPosition(frame int64) error {
	if frame < 0 {
		frame = 0
	}
	ms := frame * 1000 / SampleRate
	if rc := C.ad_seek(a.c, C.int64_t(ms)); rc < 0 {
		return fmt.Errorf("decoder: audio seek failed (%d)", int(rc))
	}
	return nil
}

// Len returns the total frames, or -1 if unknown.
func (a *Audio) Len() int64 {
	if a.dur <= 0 {
		return -1
	}
	return a.frames
}

// Close releases the audio decoder.
func (a *Audio) Close() {
	if a.c != nil {
		C.ad_close(a.c)
		a.c = nil
	}
}
