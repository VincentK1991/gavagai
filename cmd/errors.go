package cmd

import "errors"

// ErrNotImplemented is returned by command stubs whose functionality has not
// yet been built. It will be removed as each phase wires in real behavior.
var ErrNotImplemented = errors.New("not implemented")
