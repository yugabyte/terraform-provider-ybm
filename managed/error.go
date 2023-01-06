package managed

import (
	"net/http"
	"net/http/httputil"
	"regexp"
)

func getErrorMessage(response *http.Response, err error) string {
	errMsg := err.Error()
	if response != nil {
		request, dumpErr := httputil.DumpRequest(response.Request, true)
		if dumpErr != nil {
			additional := "Error while dumping request: " + dumpErr.Error()
			errMsg = errMsg + "\n\n\nDump error:" + additional
		} else {
			reqString := string(request)
			// Replace the Authorization Bearer header with obfuscated value
			re := regexp.MustCompile(`eyJ(.*)`)
			reqString = re.ReplaceAllString(reqString, `***`)
			errMsg = errMsg + "\n\nAPI Request:\n" + reqString
		}

		response, dumpErr := httputil.DumpResponse(response, true)
		if dumpErr != nil {
			additional := "Error while dumping response: " + dumpErr.Error()
			errMsg = errMsg + "\n\n\nDump error:" + additional
		} else {
			errMsg = errMsg + "\n\nAPI Response:\n" + string(response)
		}
	}
	return errMsg
}
