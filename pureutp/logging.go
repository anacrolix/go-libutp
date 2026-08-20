package pureutp

import "github.com/anacrolix/log"

// Logger is the default logger for Sockets. Override per Socket with WithLogger.
var Logger = log.Default.WithContextText("pureutp")
