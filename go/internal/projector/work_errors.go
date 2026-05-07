package projector

import "errors"

// ErrWorkSuperseded reports that a claimed projector generation was replaced
// by a newer same-scope generation and should stop without acking or failing.
var ErrWorkSuperseded = errors.New("projector work superseded")
