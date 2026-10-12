package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
)

// ProbeDuration returns a sound file's length from its headers, without
// decoding audio. For MP3 it reads the VBR header (Xing/Info/VBRI) or, for
// constant bit rate files, computes the length from the bit rate; at most
// the first 128 KB and the last 128 bytes are read. The exact length is
// stored once a sound has been played to its end.
func ProbeDuration(path string) (time.Duration, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if strings.EqualFold(filepath.Ext(path), ".mp3") {
		return probeMP3(f)
	}
	var (
		stream beep.StreamSeekCloser
		format beep.Format
	)
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".wav":
		stream, format, err = wav.Decode(f)
	case ".flac":
		stream, format, err = flac.Decode(f)
	case ".ogg":
		stream, format, err = vorbis.Decode(f)
	default:
		err = fmt.Errorf("unsupported file type %q", ext)
	}
	if err != nil {
		return 0, err
	}
	defer stream.Close()
	if format.SampleRate <= 0 || stream.Len() <= 0 {
		return 0, errors.New("length unknown")
	}
	return format.SampleRate.D(stream.Len()), nil
}

// MP3 layer III tables, indexed by [MPEG-1 = 0, MPEG-2/2.5 = 1].
var (
	mp3Bitrates = [2][16]int{
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
	}
	mp3Rates = map[int][3]int{3: {44100, 48000, 32000}, 2: {22050, 24000, 16000}, 0: {11025, 12000, 8000}}
)

type mp3Header struct {
	mpeg1     bool
	bitrate   int // kbit/s
	rate      int // Hz
	mono      bool
	frameSize int
}

func parseMP3Header(b []byte) (mp3Header, bool) {
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return mp3Header{}, false
	}
	version := int(b[1]>>3) & 3 // 3 = MPEG-1, 2 = MPEG-2, 0 = MPEG-2.5
	layer := int(b[1]>>1) & 3   // 1 = layer III
	bi, ri := int(b[2]>>4), int(b[2]>>2)&3
	if version == 1 || layer != 1 || bi == 0 || bi == 15 || ri == 3 {
		return mp3Header{}, false
	}
	h := mp3Header{mpeg1: version == 3, rate: mp3Rates[version][ri], mono: b[3]>>6 == 3}
	t := 1
	if h.mpeg1 {
		t = 0
	}
	h.bitrate = mp3Bitrates[t][bi]
	padding := int(b[2]>>1) & 1
	if h.mpeg1 {
		h.frameSize = 144000*h.bitrate/h.rate + padding
	} else {
		h.frameSize = 72000*h.bitrate/h.rate + padding
	}
	return h, h.frameSize > 4
}

func (h mp3Header) samplesPerFrame() int {
	if h.mpeg1 {
		return 1152
	}
	return 576
}

func probeMP3(f *os.File) (time.Duration, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	size := info.Size()
	head := make([]byte, 128<<10)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return 0, err
	}
	head = head[:n]

	// Skip an ID3v2 tag (its size is stored as 4 x 7 bits). base is the
	// file offset of head[0].
	start, base := 0, int64(0)
	if len(head) >= 10 && string(head[:3]) == "ID3" {
		start = 10 + (int(head[6])<<21 | int(head[7])<<14 | int(head[8])<<7 | int(head[9]))
		if head[5]&0x10 != 0 {
			start += 10 // footer
		}
		if start+4 > len(head) { // a large tag (cover art): read past it
			buf := make([]byte, 16<<10)
			n, err := f.ReadAt(buf, int64(start))
			if err != nil && !errors.Is(err, io.EOF) {
				return 0, err
			}
			head, base, start = buf[:n], int64(start), 0
		}
	}

	// The first frame: two consecutive valid headers, so stray 0xFF bytes
	// in the data are not mistaken for one.
	pos := -1
	var h mp3Header
	for i := start; i+4 <= len(head); i++ {
		if hh, ok := parseMP3Header(head[i:]); ok {
			if j := i + hh.frameSize; j+4 > len(head) {
				pos, h = i, hh
				break
			} else if _, ok := parseMP3Header(head[j:]); ok {
				pos, h = i, hh
				break
			}
		}
	}
	if pos < 0 {
		return 0, errors.New("no MP3 frame found")
	}
	frame := head[pos:min(len(head), pos+h.frameSize)]

	// Xing/Info header after the side information, or VBRI 32 bytes in.
	side := 32
	switch {
	case h.mpeg1 && h.mono:
		side = 17
	case !h.mpeg1 && !h.mono:
		side = 17
	case !h.mpeg1 && h.mono:
		side = 9
	}
	if x := 4 + side; len(frame) >= x+12 {
		tag := string(frame[x : x+4])
		if (tag == "Xing" || tag == "Info") && binary.BigEndian.Uint32(frame[x+4:])&1 != 0 {
			frames := int64(binary.BigEndian.Uint32(frame[x+8:]))
			return time.Duration(frames*int64(h.samplesPerFrame())) * time.Second / time.Duration(h.rate), nil
		}
	}
	if len(frame) >= 36+18 && bytes.Equal(frame[36:40], []byte("VBRI")) {
		frames := int64(binary.BigEndian.Uint32(frame[36+14:]))
		return time.Duration(frames*int64(h.samplesPerFrame())) * time.Second / time.Duration(h.rate), nil
	}

	// Constant bit rate: audio bytes / bytes per second. An ID3v1 tag at
	// the end is not audio.
	audio := size - base - int64(pos)
	tail := make([]byte, 3)
	if size >= 128 {
		if _, err := f.ReadAt(tail, size-128); err == nil && string(tail) == "TAG" {
			audio -= 128
		}
	}
	if audio <= 0 {
		return 0, errors.New("no audio")
	}
	return time.Duration(audio * 8 * int64(time.Millisecond) / int64(h.bitrate)), nil
}
