package terraformstate_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestParseStreamLargeStateDoesNotRetainProviderBindingsOrWarnings(t *testing.T) {
	const resourceInstances = 20_000

	options := parseFixtureOptions(t)
	var count int
	peakHeapGrowth := measurePeakHeapGrowth(t, func() {
		_, err := terraformstate.ParseStream(
			context.Background(),
			largeProviderBindingWarningStateReader(resourceInstances),
			options,
			terraformstate.FactSinkFunc(func(_ context.Context, envelope facts.Envelope) error {
				switch envelope.FactKind {
				case facts.TerraformStateProviderBindingFactKind, facts.TerraformStateWarningFactKind:
					count++
				}
				return nil
			}),
		)
		if err != nil {
			t.Fatalf("ParseStream() error = %v, want nil", err)
		}
	})

	if got, want := count, resourceInstances*2; got != want {
		t.Fatalf("provider/warning facts = %d, want %d", got, want)
	}
	if peakHeapGrowth > maxStreamResourcePeakHeapGrowth {
		t.Fatalf("ParseStream() peak heap growth = %d bytes, want at most %d", peakHeapGrowth, maxStreamResourcePeakHeapGrowth)
	}
}

func TestParseStreamStopsBeforeEOFWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelAfterBytesReader{
		reader:      largeProviderBindingWarningStateReader(50_000),
		cancelAfter: 8 << 10,
		cancel:      cancel,
	}

	_, err := terraformstate.ParseStream(
		ctx,
		reader,
		parseFixtureOptions(t),
		terraformstate.FactSinkFunc(func(context.Context, facts.Envelope) error {
			return nil
		}),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ParseStream() error = %v, want context.Canceled", err)
	}
	if got, max := reader.bytesRead, int64(1<<20); got > max {
		t.Fatalf("ParseStream() read %d bytes after cancellation, want at most %d", got, max)
	}
}

func largeProviderBindingWarningStateReader(instanceCount int) io.Reader {
	const prefix = `{"serial":17,"lineage":"lineage-123","resources":[`
	const suffix = `]}`
	return io.MultiReader(
		strings.NewReader(prefix),
		&providerBindingWarningReader{count: instanceCount},
		strings.NewReader(suffix),
	)
}

type providerBindingWarningReader struct {
	count     int
	written   int
	offset    int
	current   string
	needComma bool
}

func (r *providerBindingWarningReader) Read(target []byte) (int, error) {
	if len(target) == 0 {
		return 0, nil
	}
	if r.current == "" {
		if r.written >= r.count {
			return 0, io.EOF
		}
		element := fmt.Sprintf(
			`{"mode":"managed","type":"aws_instance","name":"web_%d","provider":"provider[\"registry.terraform.io/hashicorp/aws\"].alias_%d","instances":[{"attributes":{"id":"i-%d","unsafe":{"nested":"value"}}}]}`,
			r.written,
			r.written,
			r.written,
		)
		if r.needComma {
			r.current = "," + element
		} else {
			r.current = element
			r.needComma = true
		}
		r.offset = 0
		r.written++
	}
	n := copy(target, r.current[r.offset:])
	r.offset += n
	if r.offset == len(r.current) {
		r.current = ""
	}
	return n, nil
}

type cancelAfterBytesReader struct {
	reader      io.Reader
	cancelAfter int64
	cancel      context.CancelFunc
	bytesRead   int64
	canceled    bool
}

func (r *cancelAfterBytesReader) Read(target []byte) (int, error) {
	n, err := r.reader.Read(target)
	r.bytesRead += int64(n)
	if !r.canceled && r.bytesRead >= r.cancelAfter {
		r.canceled = true
		r.cancel()
	}
	return n, err
}
