package comprehensive

import "io"

// Reader defines a basic read interface.
type Reader interface {
	Read(p []byte) (n int, err error)
}

// Writer defines a basic write interface.
type Writer interface {
	Write(p []byte) (n int, err error)
}

// ReadWriter embeds Reader and Writer.
type ReadWriter interface {
	Reader
	Writer
}

// Stringer is an interface with a single method.
type Stringer interface {
	String() string
}

// Service defines a service interface.
type Service interface {
	Start() error
	Stop() error
	Health() bool
}

// Closer is a simple close interface.
type Closer interface {
	Close() error
}

// EmptyInterface demonstrates the empty interface.
type Any interface{}

// FileService implements Service and io.Closer.
type FileService struct {
	path   string
	opened bool
}

func (fs *FileService) Start() error {
	fs.opened = true
	return nil
}

func (fs *FileService) Stop() error {
	fs.opened = false
	return nil
}

func (fs *FileService) Health() bool {
	return fs.opened
}

func (fs *FileService) Close() error {
	return fs.Stop()
}

// Ensure FileService implements the interfaces.
var _ Service = (*FileService)(nil)
var _ io.Closer = (*FileService)(nil)
