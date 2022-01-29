package utp

import (
	"fmt"
	"os"
	"time"

	"github.com/anacrolix/log"
)

const (
	logCallbacks = false
	utpLogging   = false
)

var Logger = log.Logger{log.StreamLogger{
	W: os.Stderr,
	Fmt: func(msg log.Msg) []byte {
		ret := []byte(fmt.Sprintf(
			"%s go-libutp: %s",
			time.Now().Format("2006-01-02T15:04:05-0700"),
			msg.Text(),
		))

		if ret[len(ret)-1] != '\n' {
			ret = append(ret, '\n')
		}
		return ret
	},
}}
