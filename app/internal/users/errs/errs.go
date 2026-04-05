package errs

import "errors"

var ErrUserAlreadyExists = errors.New("user already exists")
var ErrUserNotFound = errors.New("user not found")
var ErrMissingData = errors.New("missing data")
var ErrUserBlocked = errors.New("user is blocked")
