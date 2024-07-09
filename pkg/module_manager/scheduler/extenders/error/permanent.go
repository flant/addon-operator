package error

// PermanentError signals that the operation should stop the module manager immediately
type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	return e.Err.Error()
}

func (e *PermanentError) Unwrap() error {
	return e.Err
}

// Permanent wraps the given err in a *PermanentError.
func Permanent(err error) *PermanentError {
	return &PermanentError{
		Err: err,
	}
}
