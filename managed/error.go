package managed

import (
	"net/http"
	"net/http/httputil"
)

func getErrorMessage(response *http.Response, err error) string {
	errMsg := []byte(err.Error())
	var dumpErr error
	if response != nil {
		errMsg, dumpErr = httputil.DumpResponse(response, true)
		if dumpErr != nil {
			errMsg = []byte("Error while dumping response: " + dumpErr.Error())
		}
	}
	return string(errMsg)
}
