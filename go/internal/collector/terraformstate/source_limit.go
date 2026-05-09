package terraformstate

import "io"

type sizeEnforcingReadCloser struct {
	reader    io.ReadCloser
	remaining int64
}

func newSizeEnforcingReadCloser(reader io.ReadCloser, maxBytes int64) io.ReadCloser {
	return &sizeEnforcingReadCloser{
		reader:    reader,
		remaining: maxBytes,
	}
}

func (r *sizeEnforcingReadCloser) Read(buffer []byte) (int, error) {
	if r.remaining < 0 {
		return 0, ErrStateTooLarge
	}
	if int64(len(buffer)) > r.remaining+1 {
		buffer = buffer[:r.remaining+1]
	}
	read, err := r.reader.Read(buffer)
	r.remaining -= int64(read)
	if r.remaining < 0 {
		return read, ErrStateTooLarge
	}
	return read, err
}

func (r *sizeEnforcingReadCloser) Close() error {
	return r.reader.Close()
}
