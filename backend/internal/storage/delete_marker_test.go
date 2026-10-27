package storage

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeleteObjectMarkerResponse(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	providers := []struct {
		name, header string
		delete       func(Settings, string) error
	}{
		{"s3", "x-amz-delete-marker", DeleteS3Object},
		{"aliyun", "x-oss-delete-marker", DeleteAliyunOSSObject},
		{"tencent", "x-cos-delete-marker", DeleteTencentCOSObject},
	}
	for _, provider := range providers {
		for _, response := range []struct {
			name   string
			status int
			marker string
			failed bool
		}{
			{"marker", http.StatusNoContent, "true", true},
			{"ordinary", http.StatusNoContent, "", false},
			{"not-marker", http.StatusNoContent, "false", false},
			{"forbidden", http.StatusForbidden, "", true},
			{"missing-marker", http.StatusNotFound, "true", true},
			{"missing", http.StatusNotFound, "", false},
		} {
			t.Run(provider.name+"/"+response.name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodDelete || r.URL.RawQuery != "" {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					}
					if response.marker != "" {
						w.Header().Set(provider.header, response.marker)
					}
					w.WriteHeader(response.status)
					if response.status == http.StatusForbidden {
						_, _ = w.Write([]byte(`<Error><Code>AccessDenied</Code><Message>denied</Message></Error>`))
					} else if response.status == http.StatusNotFound {
						_, _ = w.Write([]byte(`<Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`))
					}
				}))
				defer server.Close()
				setting := Settings{Endpoint: server.URL, Region: "test-region", Bucket: "bucket", AccessKeyID: "test-id", AccessKeySecret: "test-secret", PathStyle: true}
				err := provider.delete(setting, "owned-object")
				if (err != nil) != response.failed {
					t.Fatalf("delete error = %v, want failure = %v", err, response.failed)
				}
			})
		}
	}
}
