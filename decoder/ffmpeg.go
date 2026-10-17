package decoder

/*
#cgo CFLAGS: -I${SRCDIR}/../.tools/ffmpeg-tmp/ffmpeg-master-latest-win64-lgpl-shared/include
#cgo LDFLAGS: -L${SRCDIR}/../.tools/ffmpeg-tmp/ffmpeg-master-latest-win64-lgpl-shared/lib -lavformat -lavcodec -lavutil -lswscale -lswresample
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/imgutils.h>
#include <libswscale/swscale.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
    AVFormatContext *fc;
    AVCodecContext  *cc;
    struct SwsContext *sws;
    AVPacket *pkt;
    AVFrame  *frm;
    int stream;
    int width, height;
    int64_t durationMs;
    double fps;
    int draining; // codec flushed, only buffered frames left
} cd_t;

static void cd_close(cd_t *c) {
    if (c->sws) sws_freeContext(c->sws);
    if (c->frm) av_frame_free(&c->frm);
    if (c->pkt) av_packet_free(&c->pkt);
    if (c->cc) avcodec_free_context(&c->cc);
    if (c->fc) avformat_close_input(&c->fc);
    free(c);
}

static int cd_open(const char *path, cd_t **out, char *err, int errlen) {
    av_log_set_level(AV_LOG_ERROR);
    cd_t *c = calloc(1, sizeof(cd_t));
    if (!c) return -1;
    int e = avformat_open_input(&c->fc, path, NULL, NULL);
    if (e < 0) { av_strerror(e, err, errlen); cd_close(c); return -1; }
    e = avformat_find_stream_info(c->fc, NULL);
    if (e < 0) { av_strerror(e, err, errlen); cd_close(c); return -1; }
    c->stream = av_find_best_stream(c->fc, AVMEDIA_TYPE_VIDEO, -1, -1, NULL, 0);
    if (c->stream < 0) { snprintf(err, errlen, "no video stream"); cd_close(c); return -1; }
    AVStream *st = c->fc->streams[c->stream];
    const AVCodec *dec = avcodec_find_decoder(st->codecpar->codec_id);
    if (!dec) { snprintf(err, errlen, "no decoder"); cd_close(c); return -1; }
    c->cc = avcodec_alloc_context3(dec);
    if (!c->cc) { snprintf(err, errlen, "no memory"); cd_close(c); return -1; }
    e = avcodec_parameters_to_context(c->cc, st->codecpar);
    if (e < 0) { av_strerror(e, err, errlen); cd_close(c); return -1; }
    c->cc->thread_count = 0; // as many threads as the decoder likes
    c->cc->thread_type = FF_THREAD_FRAME | FF_THREAD_SLICE;
    e = avcodec_open2(c->cc, dec, NULL);
    if (e < 0) { av_strerror(e, err, errlen); cd_close(c); return -1; }
    c->width = c->cc->width;
    c->height = c->cc->height;
    if (c->fc->duration > 0) c->durationMs = c->fc->duration / (AV_TIME_BASE / 1000);
    if (st->duration > 0) c->durationMs = av_rescale_q(st->duration, st->time_base, (AVRational){1, 1000});
    AVRational fr = st->avg_frame_rate;
    if (fr.num > 0 && fr.den > 0) c->fps = av_q2d(fr);
    c->pkt = av_packet_alloc();
    c->frm = av_frame_alloc();
    if (!c->pkt || !c->frm) { snprintf(err, errlen, "no memory"); cd_close(c); return -1; }
    *out = c;
    return 0;
}

// cd_to_rgba converts frm into dst (width*height*4 bytes).
static void cd_to_rgba(cd_t *c, AVFrame *frm, uint8_t *dst) {
    if (!c->sws) {
        c->sws = sws_getContext(frm->width, frm->height, frm->format,
            frm->width, frm->height, AV_PIX_FMT_RGBA, SWS_BILINEAR, NULL, NULL, NULL);
    }
    uint8_t *d[4] = { dst, NULL, NULL, NULL };
    int ds[4] = { frm->width * 4, 0, 0, 0 };
    sws_scale(c->sws, (const uint8_t * const *)frm->data, frm->linesize, 0, frm->height, d, ds);
}

// cd_next decodes the next frame at or after minMs into dst, decoding and
// dropping the frames before it without converting them, as an accurate
// seek does. 1 = frame (ptsMs set), 0 = EOF, -1 = error (err set).
static int cd_next(cd_t *c, uint8_t *dst, int64_t *ptsMs, int64_t minMs, char *err, int errlen) {
    for (;;) {
        if (!c->draining) {
            int r = av_read_frame(c->fc, c->pkt);
            if (r < 0) {
                avcodec_send_packet(c->cc, NULL);
                c->draining = 1;
            } else {
                if (c->pkt->stream_index == c->stream) {
                    int s = avcodec_send_packet(c->cc, c->pkt);
                    av_packet_unref(c->pkt);
                    if (s < 0 && s != AVERROR(EAGAIN)) { av_strerror(s, err, errlen); return -1; }
                } else {
                    av_packet_unref(c->pkt);
                }
            }
        }
        int r = avcodec_receive_frame(c->cc, c->frm);
        if (r == 0) {
            AVStream *st = c->fc->streams[c->stream];
            int64_t ts = c->frm->best_effort_timestamp;
            if (ts == AV_NOPTS_VALUE) ts = c->frm->pts;
            int64_t ms = 0;
            if (ts != AV_NOPTS_VALUE) ms = av_rescale_q(ts, st->time_base, (AVRational){1, 1000});
            if (ms < minMs) { av_frame_unref(c->frm); continue; }
            *ptsMs = ms;
            cd_to_rgba(c, c->frm, dst);
            av_frame_unref(c->frm);
            return 1;
        }
        if (r == AVERROR_EOF) return 0;
        if (r == AVERROR(EAGAIN)) {
            if (c->draining) return 0;
            continue;
        }
        av_strerror(r, err, errlen);
        return -1;
    }
}

static int cd_seek(cd_t *c, int64_t ms) {
    AVStream *st = c->fc->streams[c->stream];
    int64_t ts = av_rescale_q(ms, (AVRational){1, 1000}, st->time_base);
    int e = av_seek_frame(c->fc, c->stream, ts, AVSEEK_FLAG_BACKWARD);
    if (e < 0) return e;
    avcodec_flush_buffers(c->cc);
    c->draining = 0;
    return 0;
}
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"
)

// Decoder decodes the video track of a file, one frame at a time.
type Decoder struct {
	c      *C.cd_t
	info   Info
	buf    []byte
	frame  Frame
	errbuf [256]C.char
}

// Open opens path and reads only what it needs to describe the video.
func Open(path string) (*Decoder, Info, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))

	var c *C.cd_t
	d := &Decoder{}
	if rc := C.cd_open(cpath, &c, &d.errbuf[0], C.int(len(d.errbuf))); rc < 0 {
		return nil, Info{}, fmt.Errorf("decoder: %s", C.GoString(&d.errbuf[0]))
	}
	d.c = c
	d.info = Info{
		Width:    int(c.width),
		Height:   int(c.height),
		Duration: time.Duration(c.durationMs) * time.Millisecond,
		FPS:      float64(c.fps),
	}
	d.buf = make([]byte, d.info.Width*d.info.Height*4)
	d.frame.RGBA = d.buf
	return d, d.info, nil
}

// Info returns what Open learned about the video.
func (d *Decoder) Info() Info { return d.info }

// Pixels returns the buffer Next fills with RGBA bytes, so that a caller
// that draws straight from it copies no picture.
func (d *Decoder) Pixels() []byte { return d.buf }

// Next decodes the next frame. It returns ErrEOF at the end. The returned
// frame's bytes are reused by the next call.
func (d *Decoder) Next() (*Frame, error) {
	return d.decode(-1)
}

// SeekTo moves to t and returns the first frame at or after it, decoding
// and dropping the frames between the keyframe and t.
func (d *Decoder) SeekTo(t time.Duration) (*Frame, error) {
	if rc := C.cd_seek(d.c, C.int64_t(t.Milliseconds())); rc < 0 {
		return nil, fmt.Errorf("decoder: seek failed (%d)", int(rc))
	}
	return d.decode(C.int64_t(t.Milliseconds()))
}

func (d *Decoder) decode(minMs C.int64_t) (*Frame, error) {
	var pts C.int64_t
	rc := C.cd_next(d.c, (*C.uint8_t)(unsafe.Pointer(&d.buf[0])), &pts, minMs, &d.errbuf[0], C.int(len(d.errbuf)))
	switch {
	case rc == 1:
		d.frame.PTS = time.Duration(pts) * time.Millisecond
		return &d.frame, nil
	case rc == 0:
		return nil, ErrEOF
	default:
		return nil, fmt.Errorf("decoder: %s", C.GoString(&d.errbuf[0]))
	}
}

// Seek moves to the keyframe at or before t.
func (d *Decoder) Seek(t time.Duration) error {
	if rc := C.cd_seek(d.c, C.int64_t(t.Milliseconds())); rc < 0 {
		return fmt.Errorf("decoder: seek failed (%d)", int(rc))
	}
	return nil
}

// Close releases the decoder.
func (d *Decoder) Close() {
	if d.c != nil {
		C.cd_close(d.c)
		d.c = nil
	}
}
