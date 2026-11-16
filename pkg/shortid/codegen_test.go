package shortid_test

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"testing/iotest"

	"github.com/skskuzan/rd-linkforge/pkg/shortid"
)

func TestCodeGeneratorReferenceValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want []string
	}{
		{"one", []byte{0, 0, 1}, []string{"000H32", "000H33", "000H34"}},
		{"zero", []byte{0, 0, 0}, []string{"000000", "000001"}},
		{"maximum random", []byte{0xff, 0xff, 0xff}, []string{"bVJgVU"}},
		{"masked high bits", []byte{0xf8, 0, 1}, []string{"000H32"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := bytes.NewReader(bytes.Repeat(tc.data, len(tc.want)))
			generator := shortid.NewCodeGenerator(reader)
			for i, want := range tc.want {
				got, err := generator.Next()
				if err != nil || got != want {
					t.Fatalf("Next call %d = %q, %v; want %q, nil", i+1, got, err, want)
				}
				if wantRemaining := (len(tc.want) - i - 1) * 3; reader.Len() != wantRemaining {
					t.Fatalf("remaining bytes = %d, want %d", reader.Len(), wantRemaining)
				}
			}
		})
	}
}

func TestCodeGeneratorReadsFullThreeBytes(t *testing.T) {
	reader := iotest.OneByteReader(bytes.NewReader([]byte{0, 0, 1}))
	got, err := shortid.NewCodeGenerator(reader).Next()
	if err != nil || got != "000H32" {
		t.Fatalf("Next() with short reads = %q, %v", got, err)
	}
}

type switchReader struct{ source io.Reader }

func (r *switchReader) Read(p []byte) (int, error) { return r.source.Read(p) }

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestCodeGeneratorReadFailurePreservesCounter(t *testing.T) {
	sentinel := errors.New("random source unavailable")
	for _, tc := range []struct {
		name   string
		reader io.Reader
		want   error
	}{
		{"EOF", bytes.NewReader(nil), io.EOF},
		{"short input", bytes.NewReader([]byte{1, 2}), io.ErrUnexpectedEOF},
		{"reader failure", errorReader{sentinel}, sentinel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &switchReader{source: bytes.NewReader([]byte{0, 0, 0})}
			generator := shortid.NewCodeGenerator(reader)
			if got, err := generator.Next(); err != nil || got != "000000" {
				t.Fatalf("first Next() = %q, %v", got, err)
			}
			reader.source = tc.reader
			if got, err := generator.Next(); got != "" || !errors.Is(err, tc.want) || errors.Unwrap(err) == nil {
				t.Fatalf("failed Next() = %q, %v; want empty code and wrapped %v", got, err, tc.want)
			}
			reader.source = bytes.NewReader([]byte{0, 0, 0})
			if got, err := generator.Next(); err != nil || got != "000001" {
				t.Fatalf("Next() after failure = %q, %v; want 000001, nil", got, err)
			}
		})
	}
}

func TestCodeGeneratorCounterWrap(t *testing.T) {
	const calls = 1<<16 + 1
	generator := shortid.NewCodeGenerator(bytes.NewReader(make([]byte, calls*3)))
	seen := make(map[string]bool, 1<<16)
	for i := range calls {
		got, err := generator.Next()
		if err != nil || len(got) != 6 {
			t.Fatalf("call %d = %q, %v", i, got, err)
		}
		if i < 1<<16 {
			if seen[got] {
				t.Fatalf("duplicate %q before counter wrapped at call %d", got, i)
			}
			seen[got] = true
		} else if got != "000000" {
			t.Fatalf("after wrapping: %q, want 000000", got)
		}
	}
}

func TestNoopContracts(t *testing.T) {
	codec := shortid.NoopCodec{}
	if got := codec.Encode(123); got != "" {
		t.Errorf("NoopCodec.Encode() = %q, want an empty code", got)
	}
	if value, err := codec.Decode("ignored"); value != 0 || err != nil {
		t.Errorf("NoopCodec.Decode() = %d, %v; want 0, nil", value, err)
	}
	if code, err := (shortid.NoopGenerator{}).Next(); code != "" || err != nil {
		t.Errorf("NoopGenerator.Next() = %q, %v; want empty code, nil", code, err)
	}
}
