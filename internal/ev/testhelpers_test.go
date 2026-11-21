package ev

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// decodeBody reads a JSON request body into out, ignoring malformed input so a
// fake handler never fails a test on its own.
func decodeBody(r *http.Request, out any) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// formatAmps renders an amp value the way a settings string would look.
func formatAmps(value float64) string {
	return fmt.Sprintf("%.0f", value)
}
