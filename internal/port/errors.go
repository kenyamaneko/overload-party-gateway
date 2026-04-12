package port

import "errors"

// ErrNotFound はリソースが見つからない場合のエラーです
var ErrNotFound = errors.New("not found")
