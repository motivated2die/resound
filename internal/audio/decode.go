package audio

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"
)

// SupportedExtensions lists the file types the decoder understands.
var SupportedExtensions = []string{".mp3", ".wav", ".flac", ".ogg"}

// streamOnly hides a file's Seek method. Given a seekable file, the MP3
// decoder reads and parses the whole file before the first sample (to learn
// its length); without Seek it starts right away. Durations come from the
// library's header probe instead. The buffer turns the decoder's many tiny
// reads into a few large ones.
type streamOnly struct {
	*bufio.Reader
	io.Closer
}

// ErrUnreadable means a sound file is missing, unsupported or damaged, as
// opposed to a problem with the outputs.
var ErrUnreadable = errors.New("the file is missing or can't be read")

// openDecoder opens a sound file and returns a fill function that decodes
// and resamples it to rate chunk by chunk, times gain (loudness
// normalization; 1 = unchanged). Nothing is held beyond one chunk,
// so files of any length use the same small amount of memory. Format errors
// are returned immediately. expected is the decoder's length estimate in
// frames (0 = unknown). Files that can't be opened or decoded give an error
// that matches ErrUnreadable.
func openDecoder(path string, rate int, gain float32) (fill fillFunc, expected int64, closer func(), err error) {
	stream, format, closer, err := openStream(path)
	if err != nil {
		return nil, 0, nil, err
	}

	expected = int64(max(stream.Len(), 0))
	var src beep.Streamer = stream
	if int(format.SampleRate) != rate && format.SampleRate > 0 {
		expected = expected * int64(rate) / int64(format.SampleRate)
		src = beep.Resample(resampleQuality, format.SampleRate, beep.SampleRate(rate), stream)
	}
	buf := make([][2]float64, chunkFrames)
	fill = func(dst []float32) (int, error) {
		want := min(len(dst)/2, len(buf))
		n, ok := src.Stream(buf[:want])
		if gain == 1 {
			for i, s := range buf[:n] {
				dst[2*i], dst[2*i+1] = float32(s[0]), float32(s[1])
			}
		} else {
			for i, s := range buf[:n] {
				dst[2*i], dst[2*i+1] = float32(s[0])*gain, float32(s[1])*gain
			}
		}
		if !ok || n == 0 {
			if serr := stream.Err(); serr != nil {
				return 2 * n, serr
			}
			return 2 * n, io.EOF
		}
		return 2 * n, nil
	}
	return fill, expected, closer, nil
}

// openStream opens a sound file's decoder at the file's own sample rate.
// closer releases the decoder and the file. Files that can't be opened or
// decoded give an error that matches ErrUnreadable.
func openStream(path string) (stream beep.StreamSeekCloser, format beep.Format, closer func(), err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, beep.Format{}, nil, fmt.Errorf("%w: %w", ErrUnreadable, err)
	}
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".mp3":
		stream, format, err = mp3.Decode(streamOnly{bufio.NewReaderSize(f, 64<<10), f})
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
		f.Close()
		return nil, beep.Format{}, nil, fmt.Errorf("decoding %s: %w: %w", filepath.Base(path), err, ErrUnreadable)
	}
	closer = func() {
		stream.Close()
		f.Close() // some decoders close it via Close, some don't
	}
	return stream, format, closer, nil
}

// CheckFile reports whether a sound file can be played: it decodes its
// first half second. Imports use it before keeping a downloaded file.
func CheckFile(path string) error {
	const rate = 48000
	fill, _, closer, err := openDecoder(path, rate, 1)
	if err != nil {
		return err
	}
	defer closer()
	buf := make([]float32, 2*chunkFrames)
	got := 0
	for got < rate/2 {
		n, err := fill(buf)
		got += n / 2
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("decoding %s: %w: %w", filepath.Base(path), err, ErrUnreadable)
		}
	}
	if got == 0 {
		return fmt.Errorf("%s has no audio: %w", filepath.Base(path), ErrUnreadable)
	}
	return nil
}

// openFileStreams decodes a sound file once, while it plays, into n sources
// at rate (one per output running at that rate). length is the known
// duration in frames (0 = use the decoder's estimate); onEnd receives the
// exact length after the whole file was decoded. gain scales the samples
// (loudness normalization; 1 = unchanged).
func openFileStreams(path string, rate, n int, length int64, gain float32, onEnd func(frames int64)) ([]*Source, error) {
	fill, expected, closer, err := openDecoder(path, rate, gain)
	if err != nil {
		return nil, err
	}
	if length > 0 {
		expected = length
	}
	return newStreams(filepath.Base(path), rate, n, expected, fill, closer, onEnd), nil
}
