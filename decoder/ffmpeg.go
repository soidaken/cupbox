package decoder

/*
#cgo CFLAGS: -I${SRCDIR}/../.tools/ffmpeg-min/include
#cgo LDFLAGS: -L${SRCDIR}/../.tools/ffmpeg-min/lib -lavformat -lavcodec -lavutil -lswscale -lswresample
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/imgutils.h>
#include <libswscale/swscale.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

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

    // Subtitles of the first subtitle track, as text ready to show.
    AVCodecContext *sc;
    int subStream;
    int subCount;
    char *subText[256];
    unsigned char *subPix[256];
    int subW[256], subH[256], subX[256], subY[256];
    int64_t subStart[256];
    int64_t subEnd[256];
    int subASS[256]; // 0 plain text, 1 ASS text, 2 bitmap
    int subErr;
} cd_t;

static void cd_clear_subs(cd_t *c) {
    for (int i = 0; i < c->subCount; i++) {
        free(c->subText[i]);
        c->subText[i] = NULL;
        free(c->subPix[i]);
        c->subPix[i] = NULL;
    }
    c->subCount = 0;
}

// cd_slot makes room for one more event, dropping the oldest when full.
static int cd_slot(cd_t *c) {
    if (c->subCount == 256) {
        free(c->subText[0]);
        free(c->subPix[0]);
        memmove(c->subText, c->subText + 1, sizeof(char *) * 255);
        memmove(c->subPix, c->subPix + 1, sizeof(unsigned char *) * 255);
        memmove(c->subW, c->subW + 1, sizeof(int) * 255);
        memmove(c->subH, c->subH + 1, sizeof(int) * 255);
        memmove(c->subX, c->subX + 1, sizeof(int) * 255);
        memmove(c->subY, c->subY + 1, sizeof(int) * 255);
        memmove(c->subStart, c->subStart + 1, sizeof(int64_t) * 255);
        memmove(c->subEnd, c->subEnd + 1, sizeof(int64_t) * 255);
        memmove(c->subASS, c->subASS + 1, sizeof(int) * 255);
        c->subCount = 255;
    }
    int i = c->subCount++;
    c->subText[i] = NULL;
    c->subPix[i] = NULL;
    c->subW[i] = c->subH[i] = c->subX[i] = c->subY[i] = 0;
    return i;
}

// cd_push_bmp queues a picture subtitle: already RGBA, at x, y of the
// video's frame.
static void cd_push_bmp(cd_t *c, int64_t start, int64_t end, int x, int y, int w, int h, unsigned char *rgba) {
    for (int i = c->subCount - 1; i >= 0 && i >= c->subCount - 64; i--) {
        if (c->subASS[i] == 2 && c->subStart[i] == start && c->subEnd[i] == end &&
            c->subX[i] == x && c->subY[i] == y && c->subW[i] == w && c->subH[i] == h) {
            free(rgba); // read again after a seek
            return;
        }
    }
    int i = cd_slot(c);
    c->subStart[i] = start;
    c->subEnd[i] = end;
    c->subASS[i] = 2;
    c->subPix[i] = rgba;
    c->subW[i] = w;
    c->subH[i] = h;
    c->subX[i] = x;
    c->subY[i] = y;
}

static void cd_push_sub(cd_t *c, int64_t start, int64_t end, int ass, const char *text) {
    for (int i = c->subCount - 1; i >= 0 && i >= c->subCount - 64; i--) {
        if (c->subASS[i] != 2 && c->subStart[i] == start && c->subEnd[i] == end &&
            c->subText[i] && !strcmp(c->subText[i], text)) {
            return; // read again after a seek
        }
    }
    int i = cd_slot(c);
    c->subText[i] = strdup(text);
    c->subStart[i] = start;
    c->subEnd[i] = end;
    c->subASS[i] = ass;
}

// cd_close_last_bmp ends the picture subtitle still showing at t: picture
// subtitles stay until the next display set, not for a fixed time.
static void cd_close_last_bmp(cd_t *c, int64_t t) {
    for (int i = c->subCount - 1; i >= 0; i--) {
        if (c->subASS[i] != 2) continue;
        if (c->subEnd[i] > t) c->subEnd[i] = t;
        return;
    }
}

// cd_decode_sub turns one subtitle packet into events. Subtitle packets carry
// their text; the times come from the packet, since most decoders leave them
// out.
static void cd_decode_sub(cd_t *c, AVPacket *pkt) {
    AVSubtitle sub = {0};
    int got = 0;
    if (avcodec_decode_subtitle2(c->sc, &sub, &got, pkt) < 0 || !got) {
        avsubtitle_free(&sub);
        return;
    }
    AVStream *st = c->fc->streams[c->subStream];
    int64_t start = 0, end = 0;
    if (pkt->pts != AV_NOPTS_VALUE) {
        start = av_rescale_q(pkt->pts, st->time_base, (AVRational){1, 1000});
    }
    if (pkt->duration > 0) {
        end = start + av_rescale_q(pkt->duration, st->time_base, (AVRational){1, 1000});
    }
    if (sub.end_display_time > sub.start_display_time) {
        start += sub.start_display_time;
        end = start + (sub.end_display_time - sub.start_display_time);
    }
    if (end <= start || end - start > 60000) end = start + 3000; // closed by the next event
    if (sub.num_rects == 0) { // a display set that clears the screen
        cd_close_last_bmp(c, start);
        avsubtitle_free(&sub);
        return;
    }
    for (unsigned i = 0; i < sub.num_rects; i++) {
        AVSubtitleRect *r = sub.rects[i];
        if (r->type == SUBTITLE_BITMAP && r->data[0] && r->data[1] && r->w > 0 && r->h > 0) {
            uint32_t *pal = (uint32_t *)r->data[1];
            unsigned char *rgba = malloc((size_t)r->w * r->h * 4);
            if (rgba) {
                for (int yy = 0; yy < r->h; yy++) {
                    const uint8_t *src = r->data[0] + (size_t)yy * r->linesize[0];
                    unsigned char *dst = rgba + (size_t)yy * r->w * 4;
                    for (int xx = 0; xx < r->w; xx++) {
                        uint32_t v = pal[src[xx]];
                        dst[xx * 4 + 0] = (v >> 16) & 0xFF; // red
                        dst[xx * 4 + 1] = (v >> 8) & 0xFF;  // green
                        dst[xx * 4 + 2] = v & 0xFF;         // blue
                        dst[xx * 4 + 3] = (v >> 24) & 0xFF; // alpha
                    }
                }
                cd_close_last_bmp(c, start);
                cd_push_bmp(c, start, end, r->x, r->y, r->w, r->h, rgba);
            }
            continue;
        }
        const char *text = r->ass ? r->ass : r->text;
        if (!text || !*text) continue;
        cd_push_sub(c, start, end, r->ass ? 1 : 0, text);
    }
    avsubtitle_free(&sub);
}

static void cd_close(cd_t *c) {
    cd_clear_subs(c);
    if (c->sc) avcodec_free_context(&c->sc);
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
    c->cc->thread_count = 8; // enough for 1080p, and far less memory than one per core
    c->cc->thread_type = FF_THREAD_FRAME | FF_THREAD_SLICE;
    e = avcodec_open2(c->cc, dec, NULL);
    if (e < 0) { av_strerror(e, err, errlen); cd_close(c); return -1; }
    c->width = c->cc->width;
    c->height = c->cc->height;
    if (c->fc->duration > 0) c->durationMs = c->fc->duration / (AV_TIME_BASE / 1000);
    if (st->duration > 0) c->durationMs = av_rescale_q(st->duration, st->time_base, (AVRational){1, 1000});
    AVRational fr = st->avg_frame_rate;
    if (fr.num > 0 && fr.den > 0) c->fps = av_q2d(fr);
    c->subStream = av_find_best_stream(c->fc, AVMEDIA_TYPE_SUBTITLE, -1, -1, NULL, 0);
    if (c->subStream >= 0) {
        AVStream *ss = c->fc->streams[c->subStream];
        const AVCodec *sdec = avcodec_find_decoder(ss->codecpar->codec_id);
        if (!sdec) c->subErr = -1000;
        if (sdec) {
            c->sc = avcodec_alloc_context3(sdec);
            if (c->sc) {
                c->subErr = avcodec_parameters_to_context(c->sc, ss->codecpar);
                if (c->subErr >= 0) c->subErr = avcodec_open2(c->sc, sdec, NULL);
                if (c->subErr < 0) avcodec_free_context(&c->sc);
            }
        }
    }
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
                } else if (c->pkt->stream_index == c->subStream && c->sc) {
                    cd_decode_sub(c, c->pkt);
                    av_packet_unref(c->pkt);
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

// cd_prime_subs decodes the subtitles that show at ms, starting 15 seconds
// before it: the packets of a line that began earlier than the picture's
// keyframe would otherwise never be decoded, and stepping into the middle
// of a line would show nothing.
static void cd_prime_subs(cd_t *c, int64_t ms) {
    if (!c->sc || c->subStream < 0) return;
    AVStream *st = c->fc->streams[c->subStream];
    int64_t back = ms - 15000;
    if (back < 0) back = 0;
    int64_t ts = av_rescale_q(back, (AVRational){1, 1000}, st->time_base);
    if (av_seek_frame(c->fc, c->subStream, ts, AVSEEK_FLAG_BACKWARD) < 0) return;
    avcodec_flush_buffers(c->sc);
    AVPacket *pkt = av_packet_alloc();
    if (!pkt) return;
    while (av_read_frame(c->fc, pkt) >= 0) {
        AVStream *ps = c->fc->streams[pkt->stream_index];
        int64_t pms = 0;
        if (pkt->pts != AV_NOPTS_VALUE) pms = av_rescale_q(pkt->pts, ps->time_base, (AVRational){1, 1000});
        if (pkt->stream_index == c->subStream) cd_decode_sub(c, pkt);
        av_packet_unref(pkt);
        if (pms > ms + 1000) break;
    }
    av_packet_free(&pkt);
}

static int cd_seek(cd_t *c, int64_t ms) {
    cd_clear_subs(c);
    if (c->sc) cd_prime_subs(c, ms);
    AVStream *st = c->fc->streams[c->stream];
    int64_t ts = av_rescale_q(ms, (AVRational){1, 1000}, st->time_base);
    int e = av_seek_frame(c->fc, c->stream, ts, AVSEEK_FLAG_BACKWARD);
    if (e < 0) return e;
    avcodec_flush_buffers(c->cc);
    if (c->sc) avcodec_flush_buffers(c->sc);
    c->draining = 0;
    return 0;
}
static int cd_has_subs(cd_t *c) { return c->sc != NULL; }
static int cd_sub_codec(cd_t *c) { return c->subStream >= 0 ? (int)c->fc->streams[c->subStream]->codecpar->codec_id : -1; }
static int cd_sub_err(cd_t *c) { return c->subStream >= 0 ? c->subErr : -1; }
static int cd_sub_width(cd_t *c) { return c->subStream >= 0 ? c->fc->streams[c->subStream]->codecpar->width : 0; }
static int cd_sub_height(cd_t *c) { return c->subStream >= 0 ? c->fc->streams[c->subStream]->codecpar->height : 0; }
static int cd_sub_count(cd_t *c) { return c->subCount; }
static int64_t cd_sub_start(cd_t *c, int i) { return c->subStart[i]; }
static int64_t cd_sub_end(cd_t *c, int i) { return c->subEnd[i]; }
static int cd_sub_is_ass(cd_t *c, int i) { return c->subASS[i]; }
static const char *cd_sub_text(cd_t *c, int i) { return c->subText[i]; }
static const unsigned char *cd_sub_pix(cd_t *c, int i) { return c->subPix[i]; }
static int cd_sub_w(cd_t *c, int i) { return c->subW[i]; }
static int cd_sub_h(cd_t *c, int i) { return c->subH[i]; }
static int cd_sub_x(cd_t *c, int i) { return c->subX[i]; }
static int cd_sub_y(cd_t *c, int i) { return c->subY[i]; }
static const char *cd_sub_header(cd_t *c, int *len) {
    if (!c->sc || !c->sc->subtitle_header) { *len = 0; return NULL; }
    *len = c->sc->subtitle_header_size;
    return (const char *)c->sc->subtitle_header;
}
static void cd_sub_take(cd_t *c, int n) {
    if (n <= 0) return;
    if (n > c->subCount) n = c->subCount;
    for (int i = 0; i < n; i++) { free(c->subText[i]); c->subText[i] = NULL; free(c->subPix[i]); c->subPix[i] = NULL; }
    memmove(c->subText, c->subText + n, sizeof(char *) * (c->subCount - n));
    memmove(c->subPix, c->subPix + n, sizeof(unsigned char *) * (c->subCount - n));
    memmove(c->subW, c->subW + n, sizeof(int) * (c->subCount - n));
    memmove(c->subH, c->subH + n, sizeof(int) * (c->subCount - n));
    memmove(c->subX, c->subX + n, sizeof(int) * (c->subCount - n));
    memmove(c->subY, c->subY + n, sizeof(int) * (c->subCount - n));
    memmove(c->subStart, c->subStart + n, sizeof(int64_t) * (c->subCount - n));
    memmove(c->subEnd, c->subEnd + n, sizeof(int64_t) * (c->subCount - n));
    memmove(c->subASS, c->subASS + n, sizeof(int) * (c->subCount - n));
    c->subCount -= n;
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
		msg := C.GoString(&d.errbuf[0])
		if msg == "no video stream" {
			return nil, Info{}, ErrNoVideo
		}
		return nil, Info{}, fmt.Errorf("decoder: %s", msg)
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

// HasSubtitles reports whether the file carries a subtitle track.
func (d *Decoder) HasSubtitles() bool { return C.cd_has_subs(d.c) != 0 }

// SubtitleCodecID returns the codec id of the subtitle track, or -1.
func (d *Decoder) SubtitleCodecID() int { return int(C.cd_sub_codec(d.c)) }

// SubtitleError returns why the subtitle track could not be opened.
func (d *Decoder) SubtitleError() int { return int(C.cd_sub_err(d.c)) }

// SubtitleSize returns the canvas picture subtitles were laid out on, which
// is not always the video's size; zero when unknown.
func (d *Decoder) SubtitleSize() (w, h int) {
	return int(C.cd_sub_width(d.c)), int(C.cd_sub_height(d.c))
}

// SubtitleEvent is one subtitle of a track, and when it shows: either text,
// or a picture at X, Y of the video's frame.
type SubtitleEvent struct {
	Start, End time.Duration
	Text       string
	ASS        bool

	Bitmap     []byte // RGBA, W×H, at X, Y of the video when not nil
	W, H, X, Y int
}

// Subtitles returns the subtitle events decoded since the last call: each
// call to Next or SeekTo decodes the packets of the subtitle track it meets
// on the way.
func (d *Decoder) Subtitles() []SubtitleEvent {
	n := int(C.cd_sub_count(d.c))
	if n == 0 {
		return nil
	}
	out := make([]SubtitleEvent, 0, n)
	for i := 0; i < n; i++ {
		idx := C.int(i)
		ev := SubtitleEvent{
			Start: time.Duration(C.cd_sub_start(d.c, idx)) * time.Millisecond,
			End:   time.Duration(C.cd_sub_end(d.c, idx)) * time.Millisecond,
		}
		switch C.cd_sub_is_ass(d.c, idx) {
		case 2:
			ev.W = int(C.cd_sub_w(d.c, idx))
			ev.H = int(C.cd_sub_h(d.c, idx))
			ev.X = int(C.cd_sub_x(d.c, idx))
			ev.Y = int(C.cd_sub_y(d.c, idx))
			if pix := C.cd_sub_pix(d.c, idx); pix != nil && ev.W > 0 && ev.H > 0 {
				ev.Bitmap = C.GoBytes(unsafe.Pointer(pix), C.int(ev.W*ev.H*4))
			}
		case 1:
			ev.Text = C.GoString(C.cd_sub_text(d.c, idx))
			ev.ASS = true
		default:
			ev.Text = C.GoString(C.cd_sub_text(d.c, idx))
		}
		out = append(out, ev)
	}
	C.cd_sub_take(d.c, C.int(n))
	return out
}

// SubtitleHeader returns the ASS header of the subtitle track, empty when
// the track carries plain text.
func (d *Decoder) SubtitleHeader() string {
	var n C.int
	p := C.cd_sub_header(d.c, &n)
	if p == nil || n <= 0 {
		return ""
	}
	return C.GoStringN(p, n)
}

// Close releases the decoder.
func (d *Decoder) Close() {
	if d.c != nil {
		C.cd_close(d.c)
		d.c = nil
	}
}
