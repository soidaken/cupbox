// Test: read the subtitle events of a file.
package main

import (
	"fmt"
	"os"
	"sort"

	"cupbox/decoder"
)

func main() {
	d, info, err := decoder.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer d.Close()
	fmt.Printf("%dx%d subs=%v codec=%d header=%d bytes\n", info.Width, info.Height,
		d.HasSubtitles(), d.SubtitleCodecID(), d.SubtitleError())
	fmt.Printf("header=%d bytes\n", len(d.SubtitleHeader()))
	var all []decoder.SubtitleEvent
	for i := 0; i < 600; i++ {
		if _, err := d.Next(); err != nil {
			break
		}
		all = append(all, d.Subtitles()...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Start < all[j].Start })
	for _, e := range all {
		fmt.Printf("%6.2fs-%6.2fs ass=%v %q\n", e.Start.Seconds(), e.End.Seconds(), e.ASS, e.Text)
	}
	fmt.Println("events:", len(all))
}
