package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
)

const profileAnalysisOperation = "asset_image_profile_analysis"
const profileAdoptionKey = "_profileAnalysisAdoption"

func profileError(status int, suffix string) error {
	return &AppError{Status: status, Code: status, Reason: kernel.ErrorReason("asset_profile_analysis_" + suffix), Message: map[string]string{"invalid_request": "分析请求格式无效", "not_found": "分析或资产不存在", "source_changed": "原始参考图已变化", "source_unavailable": "原始参考图不可用", "model_unknown": "模型能力未知", "model_unsupported": "模型不支持此图片分析", "invalid_result": "模型未返回有效的档案草稿", "not_adoptable": "当前草稿不可采用", "idempotency_conflict": "采用请求与原请求不一致", "already_adopted": "此草稿已经采用", "write_busy": "写入繁忙，请使用相同请求重试"}[suffix]}
}

// StrictProfileAnalysisJSON rejects duplicate keys at every nesting level before binding.
func StrictProfileAnalysisJSON(raw []byte, target any) error {
	if !utf8.Valid(raw) {
		return profileError(400, "invalid_request")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	var scan func() error
	scan = func() error {
		token, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return profileError(400, "invalid_request")
				}
				seen[key] = true
				if err := scan(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := scan(); err != nil {
					return err
				}
			}
		default:
			return profileError(400, "invalid_request")
		}
		_, err = d.Token()
		return err
	}
	if err := scan(); err != nil {
		return profileError(400, "invalid_request")
	}
	if _, err := d.Token(); err != io.EOF {
		return profileError(400, "invalid_request")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return profileError(400, "invalid_request")
	}
	return nil
}

func profileFields(category model.AssetCategory) []string {
	switch category {
	case model.AssetCategoryCharacter:
		return []string{"appearance", "physique", "clothing", "props"}
	case model.AssetCategoryEnvironment, model.AssetCategoryProp:
		return []string{"description", "prompt"}
	default:
		return nil
	}
}

func parseProfileDraft(raw string, category model.AssetCategory) (map[string]string, error) {
	fail := func() (map[string]string, error) { return nil, profileError(422, "invalid_result") }
	if len(raw) > 32<<10 || !utf8.ValidString(raw) {
		return fail()
	}
	var values map[string]json.RawMessage
	if StrictProfileAnalysisJSON([]byte(raw), &values) != nil || values == nil {
		return fail()
	}
	fields := profileFields(category)
	if len(fields) == 0 || len(values) != len(fields) {
		return fail()
	}
	draft := make(map[string]string, len(fields))
	for _, field := range fields {
		value, ok := values[field]
		if !ok || len(value) == 0 || value[0] != '"' {
			return fail()
		}
		var text string
		if json.Unmarshal(value, &text) != nil {
			return fail()
		}
		for _, r := range text {
			if (r < 32 && r != '\n' && r != '\r' && r != '\t') || r == 127 {
				return fail()
			}
		}
		text = strings.TrimSpace(text)
		if utf8.RuneCountInString(text) > 2000 {
			return fail()
		}
		draft[field] = text
	}
	return draft, nil
}

func validProfileID(id string, max int) bool {
	if id == "" || strings.TrimSpace(id) != id || len(id) > max {
		return false
	}
	for _, r := range id {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}
