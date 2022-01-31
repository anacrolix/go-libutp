package utp

import "github.com/anacrolix/log"

const (
	logCallbacks = false
	utpLogging   = false
)

var Logger = log.Default.WithContextText("go-libutp")
