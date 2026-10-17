package main

/*
#cgo CFLAGS: -I${SRCDIR}/../../.tools/ffmpeg-tmp/ffmpeg-master-latest-win64-lgpl-shared/include
#cgo LDFLAGS: -L${SRCDIR}/../../.tools/ffmpeg-tmp/ffmpeg-master-latest-win64-lgpl-shared/lib -lavformat -lavcodec -lavutil -lswscale -lswresample
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/imgutils.h>
#include <libswscale/swscale.h>
#include <stdlib.h>

typedef struct {
    AVFormatContext *fc;
    AVCodecContext  *cc;
    int stream;
    int width, height;
} probe_ctx;

static int probe_open(const char *path, probe_ctx *p) {
    int err = avformat_open_input(&p->fc, path, NULL, NULL);
    if (err < 0) return err;
    err = avformat_find_stream_info(p->fc, NULL);
    if (err < 0) return err;
    p->stream = av_find_best_stream(p->fc, AVMEDIA_TYPE_VIDEO, -1, -1, NULL, 0);
    if (p->stream < 0) return p->stream;
    AVStream *st = p->fc->streams[p->stream];
    const AVCodec *dec = avcodec_find_decoder(st->codecpar->codec_id);
    if (!dec) return AVERROR_DECODER_NOT_FOUND;
    p->cc = avcodec_alloc_context3(dec);
    if (!p->cc) return AVERROR(ENOMEM);
    err = avcodec_parameters_to_context(p->cc, st->codecpar);
    if (err < 0) return err;
    err = avcodec_open2(p->cc, dec, NULL);
    if (err < 0) return err;
    p->width = p->cc->width;
    p->height = p->cc->height;
    return 0;
}

static void probe_close(probe_ctx *p) {
    if (p->cc) avcodec_free_context(&p->cc);
    if (p->fc) avformat_close_input(&p->fc);
}

// probe_first_frame decodes up to the first frame and converts it to RGBA.
// Returns 0 on success and fills out/rw/rh with a malloc'd buffer.
static int probe_first_frame(probe_ctx *p, uint8_t **out, int *rw, int *rh) {
    AVPacket *pkt = av_packet_alloc();
    AVFrame *frm = av_frame_alloc();
    int err, got = 0;
    while (av_read_frame(p->fc, pkt) >= 0) {
        if (pkt->stream_index != p->stream) { av_packet_unref(pkt); continue; }
        if (avcodec_send_packet(p->cc, pkt) < 0) { av_packet_unref(pkt); continue; }
        av_packet_unref(pkt);
        err = avcodec_receive_frame(p->cc, frm);
        if (err == 0) { got = 1; break; }
        if (err == AVERROR(EAGAIN)) continue;
        break;
    }
    if (!got) { av_frame_free(&frm); av_packet_free(&pkt); return AVERROR(EAGAIN); }

    int w = frm->width, h = frm->height;
    uint8_t *buf = malloc((size_t)w * h * 4);
    uint8_t *dst[4] = { buf, NULL, NULL, NULL };
    int dstStride[4] = { w * 4, 0, 0, 0 };
    struct SwsContext *sws = sws_getContext(w, h, frm->format,
        w, h, AV_PIX_FMT_RGBA, SWS_BILINEAR, NULL, NULL, NULL);
    sws_scale(sws, (const uint8_t * const *)frm->data, frm->linesize, 0, h, dst, dstStride);
    sws_freeContext(sws);
    *out = buf; *rw = w; *rh = h;
    av_frame_free(&frm); av_packet_free(&pkt);
    return 0;
}
*/
import "C"

import (
	"fmt"
	"os"
	"time"
	"unsafe"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: ffmpeg-probe <file.mp4>")
		os.Exit(2)
	}
	path := os.Args[1]

	t0 := time.Now()
	var p C.probe_ctx
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	if rc := C.probe_open(cpath, &p); rc < 0 {
		fmt.Println("open failed:", rc)
		os.Exit(1)
	}
	tOpen := time.Since(t0)
	defer C.probe_close(&p)

	t1 := time.Now()
	var buf *C.uint8_t
	var w, h C.int
	if rc := C.probe_first_frame(&p, &buf, &w, &h); rc < 0 {
		fmt.Println("decode failed:", rc)
		os.Exit(1)
	}
	tFrame := time.Since(t1)
	defer C.free(unsafe.Pointer(buf))

	fmt.Printf("size=%dx%d open=%v firstframe=%v total=%v\n",
		p.width, p.height, tOpen.Round(time.Microsecond), tFrame.Round(time.Microsecond),
		time.Since(t0).Round(time.Microsecond))
}
