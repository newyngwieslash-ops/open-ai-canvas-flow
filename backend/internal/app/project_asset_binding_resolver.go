package app

import (
	"sort"
	"strings"
	"yingce/backend/internal/model"
)

type storyboardAssetReference struct {
	AssetID  string
	Category BindingCategory
	Name     string
}

// 仅接收服务端已鉴权、完整加载的项目目录；媒体可用性由快照读取方核验。
type storyboardAssetCatalog struct {
	ProjectID string
	Assets    []storyboardAssetSnapshot
}

type storyboardAssetSnapshot struct {
	ProjectID       string
	Asset           model.Asset
	Versions        []model.AssetVersion
	ImageVersionIDs map[string]bool
}

func resolveStoryboardAssetReference(ref storyboardAssetReference, catalog storyboardAssetCatalog, allowedIDs map[string]bool) BindingCandidate {
	result := BindingCandidate{Category: ref.Category, Name: strings.TrimSpace(ref.Name), Evidence: []BindingEvidence{}, Issues: []BindingIssue{}}
	reject := func(kind, message string) BindingCandidate {
		result.Issues = append(result.Issues, BindingIssue{Kind: kind, Message: message})
		return result
	}
	if ref.Category != "character" && ref.Category != "environment" && ref.Category != "prop" {
		return reject("category_mismatch", "资产类别不支持")
	}
	var matches []storyboardAssetSnapshot
	for _, entry := range catalog.Assets {
		if catalog.ProjectID == "" || entry.ProjectID != catalog.ProjectID {
			continue
		}
		if ref.AssetID != "" {
			if entry.Asset.ID == ref.AssetID {
				matches = append(matches, entry)
			}
		} else if result.Name != "" && BindingCategory(entry.Asset.Category) == ref.Category && strings.TrimSpace(entry.Asset.Title) == result.Name {
			matches = append(matches, entry)
		}
	}
	if len(matches) == 0 {
		if ref.AssetID != "" {
			return reject("invalid_id", "资产标识不在当前项目目录中")
		}
		return reject("unmatched", "名称未精确匹配当前项目资产")
	}
	if len(matches) != 1 {
		return reject("ambiguous", "完整项目目录中存在多个匹配")
	}
	entry := matches[0]
	if BindingCategory(entry.Asset.Category) != ref.Category {
		return reject("category_mismatch", "资产标识与类别不符")
	}
	// allowedIDs 只来自服务端任务上下文；不得先过滤目录再判断名称唯一性。
	if allowedIDs != nil && !allowedIDs[entry.Asset.ID] {
		return reject("not_allowed", "资产不在本次任务允许范围内")
	}
	result.AssetID, result.Name = entry.Asset.ID, strings.TrimSpace(entry.Asset.Title)
	kind, field := "exact_name", "name"
	if ref.AssetID != "" {
		kind, field = "structured_id", "assetId"
	}
	result.Evidence = append(result.Evidence, BindingEvidence{Kind: kind, Field: field})
	validStatus := func(status model.AssetVersionStatus) bool {
		return status == model.AssetVersionStatusDraft || status == model.AssetVersionStatusReview || status == model.AssetVersionStatusConfirmed
	}
	if validStatus(entry.Asset.Status) && entry.Asset.PrimaryVersionID != "" {
		for _, version := range entry.Versions {
			if version.ID == entry.Asset.PrimaryVersionID && version.AssetID == entry.Asset.ID && validStatus(version.Status) {
				id := version.ID
				result.AssetVersionID = &id
				result.Bindable = true
				result.DefaultSelected = ref.AssetID != ""
				result.MissingImage = !entry.ImageVersionIDs[id]
				if result.MissingImage {
					return reject("missing_image", "有效版本暂无可用参考图")
				}
				return result
			}
		}
	}
	return reject("missing_version", "资产没有有效的当前版本")
}

type storyboardStructuredEvidence struct {
	Reference    storyboardAssetReference
	Kind         string
	Field        string
	SourceShotID string
}

type storyboardAssetCapacity struct {
	ExistingReferences   []model.ShotAssetReference
	Candidates           []BindingCandidate
	ExistingVersionCount int
	RemainingCapacity    int
	TotalVersionCount    int
	Issues               []BindingIssue
}

func extractStoryboardAssetEvidence(description string, revision model.ShotRevision, catalog storyboardAssetCatalog, structured []storyboardStructuredEvidence) []BindingCandidate {
	results := make([]BindingCandidate, 0)
	for _, item := range structured {
		candidate := resolveStoryboardAssetReference(item.Reference, catalog, nil)
		candidate.Evidence = []BindingEvidence{{Kind: item.Kind, Field: item.Field}}
		visual := item.Kind == "visual_label" || (item.Kind == "structured_id" && item.Reference.AssetID != "")
		if item.Kind == "structured_continuity" {
			visual = strings.TrimSpace(item.SourceShotID) != "" && item.Reference.AssetID != ""
			if !visual {
				candidate.Issues = append(candidate.Issues, BindingIssue{Kind: "context_warning", Message: "连续性缺少可追溯的镜头及资产标识"})
			}
		}
		candidate.DefaultSelected = candidate.Bindable && visual
		results = append(results, candidate)
	}
	fields := []struct{ name, text string }{
		{"description", description}, {"plotDescription", revision.PlotDescription}, {"action", revision.Action}, {"dialogue", revision.Dialogue},
		{"imagePrompt", revision.ImagePrompt}, {"videoPrompt", revision.VideoPrompt}, {"continuityNotes", revision.ContinuityNotes},
	}
	// 索引覆盖完整目录，不应用任务摘要的 allowedIds，也不推断别名或未出现的道具。
	names := make(map[string][]storyboardAssetSnapshot)
	for _, entry := range catalog.Assets {
		name := strings.TrimSpace(entry.Asset.Title)
		if entry.ProjectID == catalog.ProjectID && name != "" && (entry.Asset.Category == "character" || entry.Asset.Category == "environment" || entry.Asset.Category == "prop") {
			names[name] = append(names[name], entry)
		}
	}
	for _, field := range fields {
		text := []rune(field.text)
		// 整行精确标签先限定类别；同类别长短名和同名仍参与后续歧义判断。
		labelCategories := make(map[int]model.AssetCategory)
		if field.name != "dialogue" && field.name != "continuityNotes" {
			lineStart := 0
			for _, line := range strings.Split(field.text, "\n") {
				trimmed := strings.TrimSpace(line)
				for label, category := range map[string]model.AssetCategory{"角色": "character", "场景": "environment", "道具": "prop"} {
					for _, separator := range []string{"：", ":"} {
						value, ok := strings.CutPrefix(trimmed, label+separator)
						if !ok {
							continue
						}
						for _, entry := range names[strings.TrimSpace(value)] {
							if entry.Asset.Category == category {
								for pos := lineStart; pos < lineStart+len([]rune(line)); pos++ {
									labelCategories[pos] = category
								}
								break
							}
						}
					}
				}
				lineStart += len([]rune(line)) + 1
			}
		}
		type occurrence struct {
			start, end int
			entry      storyboardAssetSnapshot
		}
		var hits []occurrence
		for name, entries := range names {
			length := len([]rune(name))
			for start := 0; start+length <= len(text); start++ {
				if string(text[start:start+length]) == name {
					for _, entry := range entries {
						if category, labeled := labelCategories[start]; labeled && entry.Asset.Category != category {
							continue
						}
						hits = append(hits, occurrence{start, start + length, entry})
					}
				}
			}
		}
		sort.Slice(hits, func(i, j int) bool {
			if hits[i].start != hits[j].start {
				return hits[i].start < hits[j].start
			}
			if hits[i].end != hits[j].end {
				return hits[i].end > hits[j].end
			}
			return hits[i].entry.Asset.ID < hits[j].entry.Asset.ID
		})
		for i, hit := range hits {
			candidate := resolveStoryboardAssetReference(storyboardAssetReference{AssetID: hit.entry.Asset.ID, Category: BindingCategory(hit.entry.Asset.Category)}, catalog, nil)
			kind := "mention"
			lineStart, lineEnd := hit.start, hit.end
			for lineStart > 0 && text[lineStart-1] != '\n' {
				lineStart--
			}
			for lineEnd < len(text) && text[lineEnd] != '\n' {
				lineEnd++
			}
			prefix, suffix := strings.TrimSpace(string(text[lineStart:hit.start])), strings.TrimSpace(string(text[hit.end:lineEnd]))
			if field.name == "dialogue" && prefix == "" && (strings.HasPrefix(suffix, "：") || strings.HasPrefix(suffix, ":")) {
				kind = "speaker"
			}
			label := map[model.AssetCategory]string{"character": "角色", "environment": "场景", "prop": "道具"}[hit.entry.Asset.Category]
			if field.name != "dialogue" && field.name != "continuityNotes" && suffix == "" && (prefix == label+"：" || prefix == label+":") {
				kind = "visual_label"
			}
			candidate.Evidence = []BindingEvidence{{Kind: kind, Field: field.name, Start: hit.start, End: hit.end, Text: string(text[hit.start:hit.end])}}
			ambiguous := false
			for j, other := range hits {
				if i != j && hit.start < other.end && other.start < hit.end {
					ambiguous = true
					break
				}
			}
			if ambiguous {
				candidate.Issues = append(candidate.Issues, BindingIssue{Kind: "ambiguous", Message: "姓名重叠或同名，无法唯一确定身份"})
				candidate.Bindable = false
			}
			// 有限词表只提示可疑上下文，不声称完整理解否定或在场语义。
			warning := false
			line := string(text[lineStart:lineEnd])
			// 显式标题的跨行作用域不明确时保守保留警告，不跨字段或向前影响已出现的证据。
			for _, previous := range strings.Split(string(text[:lineStart]), "\n") {
				previous = strings.TrimSpace(previous)
				if strings.HasSuffix(previous, "：") || strings.HasSuffix(previous, ":") {
					line += "\n" + previous
				}
			}
			for _, marker := range []string{"不", "未", "没有", "无", "回忆", "回想", "旁白", "画外音"} {
				if strings.Contains(line, marker) {
					warning = true
					break
				}
			}
			if warning {
				candidate.Issues = append(candidate.Issues, BindingIssue{Kind: "context_warning", Message: "包含否定、回忆或非画内语境，请核对原文"})
			}
			candidate.DefaultSelected = candidate.Bindable && kind == "visual_label" && !warning
			results = append(results, candidate)
		}
	}
	return results
}

func applyStoryboardAssetCapacity(existing []model.ShotAssetReference, candidates []BindingCandidate) storyboardAssetCapacity {
	result := storyboardAssetCapacity{ExistingReferences: append([]model.ShotAssetReference(nil), existing...), Candidates: make([]BindingCandidate, len(candidates))}
	versions := make(map[string]bool)
	for _, reference := range existing {
		versions[reference.AssetVersionID] = true
	}
	result.ExistingVersionCount = len(versions)
	result.RemainingCapacity = max(0, 6-len(versions))
	for i, candidate := range candidates {
		result.Candidates[i] = candidate
		result.Candidates[i].Evidence = append([]BindingEvidence{}, candidate.Evidence...)
		result.Candidates[i].Issues = append([]BindingIssue{}, candidate.Issues...)
		if candidate.Bindable && candidate.DefaultSelected && candidate.AssetVersionID != nil {
			versions[*candidate.AssetVersionID] = true
		}
	}
	result.TotalVersionCount = len(versions)
	if result.TotalVersionCount > 6 {
		result.Issues = append(result.Issues, BindingIssue{Kind: "capacity_exceeded", Message: "已有版本与明确建议超过 6 个，请手动选择"})
		for i := range result.Candidates {
			result.Candidates[i].DefaultSelected = false
			result.Candidates[i].Issues = append(result.Candidates[i].Issues, BindingIssue{Kind: "capacity_exceeded", Message: "已有版本与明确建议超过 6 个，请手动选择"})
		}
	}
	return result
}
