package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"vivatom-api-svc/internal/usage"
)

func TestWriteUsageErrorIncludesCompileDiagnostic(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	writeUsageError(context, &usage.Error{
		Code:    "compile_failed",
		Status:  422,
		Message: "src/App.vue:12 unexpected token",
	})

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "compile_failed" || body.Error.Message != "候选源码未通过服务端隔离编译：src/App.vue:12 unexpected token" {
		t.Fatalf("unexpected response: %+v", body.Error)
	}
}
