package managed

import (
	"net/http"
	"net/http/httputil"
)

func getErrorMessage(response *http.Response, err error) string {
	errMsg := []byte(err.Error())
	if response != nil {
		errMsg, _ = httputil.DumpResponse(response, true)
	}
	return string(errMsg)
}
