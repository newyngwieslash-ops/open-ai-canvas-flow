package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type BindingCategory string

type BindingEvidence struct {
	Kind  string `json:"kind"`
	Field string `json:"field"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

type BindingIssue struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type BindingCandidate struct {
	AssetID         string            `json:"assetId"`
	Category        BindingCategory   `json:"category"`
	Name            string            `json:"name"`
	AssetVersionID  *string           `json:"assetVersionId"`
	Evidence        []BindingEvidence `json:"evidence"`
	Issues          []BindingIssue    `json:"issues"`
	Bindable        bool              `json:"bindable"`
	MissingImage    bool              `json:"missingImage"`
	DefaultSelected bool              `json:"defaultSelected"`
}

type BindingPreviewRequest struct {
	ShotIDs []string `json:"shotIds"`
}
type BindingReference struct {
	ReferenceID    string `json:"referenceId"`
	AssetVersionID string `json:"assetVersionId"`
	Role           string `json:"role"`
	Status         string `json:"status"`
}
type BindingArtifact struct {
	ArtifactID         string `json:"artifactId"`
	ContentFingerprint string `json:"contentFingerprint"`
	Status             string `json:"status"`
	Selected           bool   `json:"selected"`
}
type BindingShotBaseline struct {
	ShotID              string             `json:"shotId"`
	ShotFingerprint     string             `json:"shotFingerprint"`
	RevisionFingerprint string             `json:"revisionFingerprint"`
	References          []BindingReference `json:"references"`
	Artifacts           []BindingArtifact  `json:"artifacts"`
}
type BindingVersionBaseline struct {
	AssetVersionID     string `json:"assetVersionId"`
	VersionFingerprint string `json:"versionFingerprint"`
	MediaFingerprint   string `json:"mediaFingerprint"`
	Bindable           bool   `json:"bindable"`
	MissingImage       bool   `json:"missingImage"`
}
type BindingCatalogBaseline struct {
	AssetID          string                   `json:"assetId"`
	AssetFingerprint string                   `json:"assetFingerprint"`
	LinkFingerprint  string                   `json:"linkFingerprint"`
	Versions         []BindingVersionBaseline `json:"versions"`
}
type BindingBaseline struct {
	ProjectID       string                   `json:"projectId"`
	UnitID          string                   `json:"unitId"`
	ProjectRevision int64                    `json:"projectRevision"`
	ShotIDs         []string                 `json:"shotIds"`
	Shots           []BindingShotBaseline    `json:"shots"`
	Catalog         []BindingCatalogBaseline `json:"catalog"`
}
type BindingShotPreview struct {
	ShotID               string             `json:"shotId"`
	ExistingReferences   []BindingReference `json:"existingReferences"`
	Candidates           []BindingCandidate `json:"candidates"`
	Issues               []BindingIssue     `json:"issues"`
	ExistingVersionCount int                `json:"existingVersionCount"`
	RemainingCapacity    int                `json:"remainingCapacity"`
}
type BindingPreview struct {
	Baseline BindingBaseline      `json:"baseline"`
	Shots    []BindingShotPreview `json:"shots"`
}

type BindingSelection struct {
	ShotID          string   `json:"shotId"`
	AssetVersionIDs []string `json:"assetVersionIds"`
}

type BindingAppendRequest struct {
	Baseline   BindingBaseline    `json:"baseline"`
	Selections []BindingSelection `json:"selections"`
}

type BindingShotAppendResult struct {
	ShotID          string             `json:"shotId"`
	AddedCount      int                `json:"addedCount"`
	AddedVersionIDs []string           `json:"addedVersionIds"`
	Unchanged       bool               `json:"unchanged"`
	References      []BindingReference `json:"references"`
	Artifacts       []BindingArtifact  `json:"artifacts"`
}

type BindingAppendResult struct {
	ProjectRevision int64                     `json:"projectRevision"`
	Unchanged       bool                      `json:"unchanged"`
	AddedCount      int                       `json:"addedCount"`
	Shots           []BindingShotAppendResult `json:"shots"`
}

const maxShotAssetReferences = 6

func validateBindingBaseline(projectID, unitID string, b BindingBaseline) error {
	if b.ProjectID != projectID || b.UnitID != unitID {
		return StoryboardAssetBindingInvalidRequest("基准归属与请求路径不一致")
	}
	if b.ProjectRevision < 0 {
		return StoryboardAssetBindingInvalidRequest("基准项目版本不合法")
	}
	if len(b.ShotIDs) == 0 || len(b.ShotIDs) > 100 {
		return StoryboardAssetBindingInvalidRequest("单次批量确认仅支持 1 至 100 个镜头")
	}
	if b.Shots == nil || b.Catalog == nil {
		return StoryboardAssetBindingInvalidRequest("基准结构不能为空")
	}
	seenShot := make(map[string]bool, len(b.ShotIDs))
	for i, id := range b.ShotIDs {
		if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) || seenShot[id] {
			return StoryboardAssetBindingInvalidRequest("基准镜头标识不合法")
		}
		if i > 0 && b.ShotIDs[i-1] >= id {
			return StoryboardAssetBindingInvalidRequest("基准镜头列表顺序不合法")
		}
		seenShot[id] = true
	}
	if len(b.Shots) != len(b.ShotIDs) {
		return StoryboardAssetBindingInvalidRequest("基准镜头明细与镜头列表不一致")
	}
	for i, shotBaseline := range b.Shots {
		if shotBaseline.ShotID != b.ShotIDs[i] {
			return StoryboardAssetBindingInvalidRequest("基准镜头明细与镜头列表不一致")
		}
		if strings.TrimSpace(shotBaseline.ShotFingerprint) == "" || strings.TrimSpace(shotBaseline.RevisionFingerprint) == "" {
			return StoryboardAssetBindingInvalidRequest("基准镜头指纹不能为空")
		}
		if shotBaseline.References == nil || shotBaseline.Artifacts == nil {
			return StoryboardAssetBindingInvalidRequest("基准镜头引用或成果列表不能为空")
		}
		for _, ref := range shotBaseline.References {
			if strings.TrimSpace(ref.ReferenceID) == "" || strings.TrimSpace(ref.AssetVersionID) == "" {
				return StoryboardAssetBindingInvalidRequest("基准镜头引用标识不能为空")
			}
		}
		for _, art := range shotBaseline.Artifacts {
			if strings.TrimSpace(art.ArtifactID) == "" || strings.TrimSpace(art.ContentFingerprint) == "" {
				return StoryboardAssetBindingInvalidRequest("基准镜头成果标识或指纹不能为空")
			}
		}
	}
	for i, item := range b.Catalog {
		if strings.TrimSpace(item.AssetID) == "" || strings.TrimSpace(item.AssetFingerprint) == "" || strings.TrimSpace(item.LinkFingerprint) == "" {
			return StoryboardAssetBindingInvalidRequest("基准资产目录指纹不能为空")
		}
		if i > 0 && b.Catalog[i-1].AssetID >= item.AssetID {
			return StoryboardAssetBindingInvalidRequest("基准资产目录顺序不合法")
		}
		if item.Versions == nil {
			return StoryboardAssetBindingInvalidRequest("基准资产版本列表不能为空")
		}
		for j, ver := range item.Versions {
			if strings.TrimSpace(ver.AssetVersionID) == "" || strings.TrimSpace(ver.VersionFingerprint) == "" || strings.TrimSpace(ver.MediaFingerprint) == "" {
				return StoryboardAssetBindingInvalidRequest("基准资产版本指纹不能为空")
			}
			if j > 0 && item.Versions[j-1].AssetVersionID >= ver.AssetVersionID {
				return StoryboardAssetBindingInvalidRequest("基准资产版本顺序不合法")
			}
		}
	}
	return nil
}

func normalizeBindingBaseline(b BindingBaseline) BindingBaseline {
	if b.ShotIDs == nil {
		b.ShotIDs = []string{}
	}
	shots := make([]BindingShotBaseline, len(b.Shots))
	for i, s := range b.Shots {
		if s.References == nil {
			s.References = []BindingReference{}
		}
		if s.Artifacts == nil {
			s.Artifacts = []BindingArtifact{}
		}
		shots[i] = s
	}
	b.Shots = shots
	catalog := make([]BindingCatalogBaseline, len(b.Catalog))
	for i, c := range b.Catalog {
		if c.Versions == nil {
			c.Versions = []BindingVersionBaseline{}
		}
		catalog[i] = c
	}
	b.Catalog = catalog
	return b
}

func isStrictExpectedPostState(cur, base BindingBaseline, addedByShot map[string][]string) bool {
	if cur.ProjectID != base.ProjectID || cur.UnitID != base.UnitID {
		return false
	}
	if cur.ProjectRevision != base.ProjectRevision+1 {
		return false
	}
	if !slices.Equal(cur.ShotIDs, base.ShotIDs) {
		return false
	}
	if !reflect.DeepEqual(cur.Catalog, base.Catalog) {
		return false
	}
	if len(cur.Shots) != len(base.Shots) {
		return false
	}

	for i := range base.Shots {
		baseShot := base.Shots[i]
		curShot := cur.Shots[i]

		if curShot.ShotID != baseShot.ShotID {
			return false
		}
		if curShot.ShotFingerprint != baseShot.ShotFingerprint {
			return false
		}
		if curShot.RevisionFingerprint != baseShot.RevisionFingerprint {
			return false
		}

		added := addedByShot[baseShot.ShotID]

		if len(curShot.References) != len(baseShot.References)+len(added) {
			return false
		}

		baseRefByID := make(map[string]BindingReference, len(baseShot.References))
		for _, ref := range baseShot.References {
			baseRefByID[ref.ReferenceID] = ref
		}

		matchedBaseIDs := make(map[string]bool, len(baseShot.References))
		newVersionIDs := make([]string, 0, len(added))
		seenNewRefIDs := make(map[string]bool, len(added))

		for _, curRef := range curShot.References {
			if baseRef, ok := baseRefByID[curRef.ReferenceID]; ok {
				if matchedBaseIDs[curRef.ReferenceID] {
					return false
				}
				if curRef != baseRef {
					return false
				}
				matchedBaseIDs[curRef.ReferenceID] = true
			} else {
				if strings.TrimSpace(curRef.ReferenceID) == "" || seenNewRefIDs[curRef.ReferenceID] {
					return false
				}
				seenNewRefIDs[curRef.ReferenceID] = true
				if curRef.Role != "reference" || curRef.Status != "linked" {
					return false
				}
				newVersionIDs = append(newVersionIDs, curRef.AssetVersionID)
			}
		}

		if len(matchedBaseIDs) != len(baseShot.References) {
			return false
		}
		if len(newVersionIDs) != len(added) {
			return false
		}
		sortedNew := append([]string(nil), newVersionIDs...)
		sortedAdded := append([]string(nil), added...)
		sort.Strings(sortedNew)
		sort.Strings(sortedAdded)
		if !slices.Equal(sortedNew, sortedAdded) {
			return false
		}

		if len(curShot.Artifacts) != len(baseShot.Artifacts) {
			return false
		}
		for j := range baseShot.Artifacts {
			baseArt := baseShot.Artifacts[j]
			curArt := curShot.Artifacts[j]

			if curArt.ArtifactID != baseArt.ArtifactID || curArt.ContentFingerprint != baseArt.ContentFingerprint {
				return false
			}
			if len(added) == 0 {
				if curArt.Status != baseArt.Status || curArt.Selected != baseArt.Selected {
					return false
				}
			} else {
				expectedStatus := "stale"
				if baseArt.Status == "failed" {
					expectedStatus = "failed"
				}
				if curArt.Status != expectedStatus || curArt.Selected {
					return false
				}
			}
		}
	}

	return true
}

func populateBindingShotAppendResults(shotResults []BindingShotAppendResult, baselineShots []BindingShotBaseline) {
	byShotID := make(map[string]BindingShotBaseline, len(baselineShots))
	for _, sb := range baselineShots {
		byShotID[sb.ShotID] = sb
	}
	for i := range shotResults {
		sb := byShotID[shotResults[i].ShotID]
		refs := sb.References
		if refs == nil {
			refs = []BindingReference{}
		}
		arts := sb.Artifacts
		if arts == nil {
			arts = []BindingArtifact{}
		}
		shotResults[i].References = refs
		shotResults[i].Artifacts = arts
	}
}

func isSQLiteBusyOrLockedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "sqlite_busy") ||
		strings.Contains(msg, "sqlite_locked")
}

func mapStoryboardBindingAppendError(err error) error {
	if err == nil {
		return nil
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		if appErr.Status == CodeUnavailable && appErr.Reason != ReasonStoryboardAssetBindingBusy {
			return StoryboardAssetBindingBusy("资产绑定繁忙，请稍后重试")
		}
		if appErr.Status == CodeConflict && appErr.Reason != ReasonStoryboardAssetBindingConflict {
			return StoryboardAssetBindingConflict("分镜或资产基准已变化，请重新预览后再确认")
		}
		return err
	}
	if isSQLiteBusyOrLockedError(err) {
		return StoryboardAssetBindingBusy("资产绑定繁忙，请稍后重试")
	}
	return err
}

func mapStoryboardBindingNotFoundError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return WrapAppError(404, "项目或章节不存在", gorm.ErrRecordNotFound)
	}
	return err
}

func (s *Service) AppendProjectUnitShotAssetBindings(ctx context.Context, userID, projectID, unitID string, req BindingAppendRequest) (BindingAppendResult, error) {
	result := BindingAppendResult{}
	if strings.TrimSpace(userID) == "" {
		return result, Unauthorized("未登录")
	}
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(unitID) == "" {
		return result, StoryboardAssetBindingInvalidRequest("项目与章节标识不能为空")
	}
	project, err := s.repo.ProjectForUser(userID, projectID)
	if err != nil {
		return result, mapStoryboardBindingNotFoundError(err)
	}
	if _, err := s.repo.ProjectUnit(projectID, unitID); err != nil {
		return result, mapStoryboardBindingNotFoundError(err)
	}
	if project.Status == model.ProjectStatusArchived {
		return result, StoryboardAssetBindingInvalidRequest("归档项目不可修改分镜资产绑定")
	}
	if err := validateBindingBaseline(projectID, unitID, req.Baseline); err != nil {
		return result, err
	}
	shotOrder := append([]string(nil), req.Baseline.ShotIDs...)
	seenShot := make(map[string]bool, len(shotOrder))
	for _, id := range shotOrder {
		seenShot[id] = true
	}
	if len(req.Selections) == 0 {
		return result, StoryboardAssetBindingInvalidRequest("镜头绑定选择列表不能为空")
	}
	selectionByShot := make(map[string][]string, len(shotOrder))
	allSelectedVersions := []string{}
	seenSelectedVersion := map[string]bool{}
	for _, sel := range req.Selections {
		if strings.TrimSpace(sel.ShotID) == "" || !seenShot[sel.ShotID] {
			return result, StoryboardAssetBindingInvalidRequest("绑定选择包含不在基准中的镜头")
		}
		deduped := make([]string, 0, len(sel.AssetVersionIDs))
		seenInShot := make(map[string]bool, len(sel.AssetVersionIDs))
		for _, vid := range sel.AssetVersionIDs {
			if strings.TrimSpace(vid) == "" || vid != strings.TrimSpace(vid) {
				return result, StoryboardAssetBindingInvalidRequest("资产版本标识不能为空")
			}
			if !seenInShot[vid] {
				seenInShot[vid] = true
				deduped = append(deduped, vid)
			}
			if !seenSelectedVersion[vid] {
				seenSelectedVersion[vid] = true
				allSelectedVersions = append(allSelectedVersions, vid)
			}
		}
		if prev, exists := selectionByShot[sel.ShotID]; exists {
			if !slices.Equal(prev, deduped) {
				return result, StoryboardAssetBindingInvalidRequest("同一镜头存在冲突的绑定选择")
			}
			continue
		}
		selectionByShot[sel.ShotID] = deduped
	}
	if len(selectionByShot) != len(shotOrder) {
		return result, StoryboardAssetBindingInvalidRequest("绑定选择未完整覆盖基准镜头")
	}

	sortedShotIDs := append([]string(nil), shotOrder...)
	normalizedBaseline := normalizeBindingBaseline(req.Baseline)

	baselineVersionSets := make(map[string]map[string]bool, len(shotOrder))
	for _, shotBaseline := range normalizedBaseline.Shots {
		vSet := make(map[string]bool, len(shotBaseline.References))
		for _, ref := range shotBaseline.References {
			if ref.AssetVersionID != "" {
				vSet[ref.AssetVersionID] = true
			}
		}
		baselineVersionSets[shotBaseline.ShotID] = vSet
	}

	addedByShot := make(map[string][]string, len(shotOrder))
	totalIntendedAdded := 0
	for _, shotID := range shotOrder {
		selected := selectionByShot[shotID]
		vSet := baselineVersionSets[shotID]
		added := []string{}
		for _, vid := range selected {
			if !vSet[vid] {
				added = append(added, vid)
			}
		}
		addedByShot[shotID] = added
		totalIntendedAdded += len(added)
	}

	initialOwners, err := s.repo.DiscoverStoryboardBindingAppendOwners(projectID, sortedShotIDs, allSelectedVersions)
	if err != nil {
		return result, mapStoryboardBindingAppendError(err)
	}
	initialOwners = append(initialOwners, userID, project.UserID)

	err = s.repo.WithStoryboardAssetOwners(ctx, initialOwners, func(tx *repository.Repository) error {
		currentProjectPtr, err := tx.ProjectForUser(userID, projectID)
		if err != nil {
			return mapStoryboardBindingNotFoundError(err)
		}
		currentProject := *currentProjectPtr
		if _, err := tx.ProjectUnit(projectID, unitID); err != nil {
			return mapStoryboardBindingNotFoundError(err)
		}
		if currentProject.Status == model.ProjectStatusArchived {
			return StoryboardAssetBindingInvalidRequest("归档项目不可修改分镜资产绑定")
		}
		if err := tx.LockStoryboardBindingAppendScope(projectID, unitID, sortedShotIDs, allSelectedVersions); err != nil {
			return err
		}

		// Verify target shots belong to (projectID, unitID) vs were deleted after preview.
		foundShots, err := tx.FindShotsByIDs(sortedShotIDs)
		if err != nil {
			return err
		}
		foundShotMap := make(map[string]model.Shot, len(foundShots))
		for _, shot := range foundShots {
			if shot.ProjectID != projectID || shot.UnitID != unitID {
				return StoryboardAssetBindingInvalidReference("所选镜头不属于当前项目或章节")
			}
			foundShotMap[shot.ID] = shot
		}
		if len(foundShotMap) != len(sortedShotIDs) {
			return StoryboardAssetBindingConflict("目标分镜集合已变化，请重新预览后再确认")
		}

		snapshot, err := tx.ReadStoryboardAssetSnapshot(projectID, unitID, sortedShotIDs)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return StoryboardAssetBindingConflict("分镜基准已变化，请重新预览后再确认")
			}
			return err
		}

		// Compare current preview baseline with request baseline (409 conflict) before snapshot-dependent 422 checks.
		currentPreview, err := buildStoryboardAssetPreview(currentProject.UserID, currentProject, unitID, sortedShotIDs, snapshot)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return StoryboardAssetBindingConflict("分镜基准已变化，请重新预览后再确认")
			}
			return err
		}

		initialMatch := reflect.DeepEqual(currentPreview.Baseline, normalizedBaseline)
		postStateMatch := false
		if !initialMatch && totalIntendedAdded > 0 {
			postStateMatch = isStrictExpectedPostState(currentPreview.Baseline, normalizedBaseline, addedByShot)
		}
		if !initialMatch && !postStateMatch {
			return StoryboardAssetBindingConflict("分镜或资产基准已变化，请重新预览后再确认")
		}

		linkedAssets := make(map[string]bool, len(snapshot.Links))
		for _, link := range snapshot.Links {
			linkedAssets[link.AssetID] = true
		}
		versionsByID := make(map[string]model.AssetVersion, len(snapshot.Versions))
		for _, v := range snapshot.Versions {
			versionsByID[v.ID] = v
		}
		assetsByID := make(map[string]model.Asset, len(snapshot.Assets))
		for _, a := range snapshot.Assets {
			assetsByID[a.ID] = a
		}

		// Validate all selected assetVersionIds across all shots (422 invalid_reference)
		for _, shotID := range shotOrder {
			for _, vid := range selectionByShot[shotID] {
				version, ok := versionsByID[vid]
				if !ok || !storyboardBindingStatus(version.Status) {
					return StoryboardAssetBindingInvalidReference("所选资产版本不可用或不属于当前项目")
				}
				asset, ok := assetsByID[version.AssetID]
				if !ok || asset.UserID != currentProject.UserID || asset.UserID != userID || !linkedAssets[asset.ID] {
					return StoryboardAssetBindingInvalidReference("所选资产版本不可用或不属于当前项目")
				}
				if asset.Category != model.AssetCategoryCharacter && asset.Category != model.AssetCategoryEnvironment && asset.Category != model.AssetCategoryProp {
					return StoryboardAssetBindingInvalidReference("所选资产分类不支持分镜绑定")
				}
				if !storyboardBindingStatus(asset.Status) {
					return StoryboardAssetBindingInvalidReference("所选资产状态不可绑定")
				}
				vCopy := version
				if err := tx.RequireAssetVersionReferences(&vCopy); err != nil {
					return StoryboardAssetBindingInvalidReference("所选资产版本引用的媒体资源不可用")
				}
			}
		}

		// Validate capacity per shot (422 capacity_exceeded)
		for _, shotID := range shotOrder {
			selected := selectionByShot[shotID]
			if len(selected) > maxShotAssetReferences {
				return StoryboardAssetBindingCapacityExceeded("单个镜头最多选择 6 个资产版本")
			}
			existingSet := baselineVersionSets[shotID]
			if len(existingSet) > maxShotAssetReferences {
				return StoryboardAssetBindingCapacityExceeded("单个镜头已有资产版本超过上限")
			}
			added := addedByShot[shotID]
			if len(existingSet)+len(added) > maxShotAssetReferences {
				return StoryboardAssetBindingCapacityExceeded("单个镜头绑定资产版本总数不能超过 6 个")
			}
		}

		if postStateMatch || totalIntendedAdded == 0 {
			shotResults := make([]BindingShotAppendResult, len(shotOrder))
			for i, shotID := range shotOrder {
				shotResults[i] = BindingShotAppendResult{
					ShotID:          shotID,
					AddedCount:      0,
					AddedVersionIDs: []string{},
					Unchanged:       true,
				}
			}
			populateBindingShotAppendResults(shotResults, currentPreview.Baseline.Shots)
			result = BindingAppendResult{
				ProjectRevision: currentProject.Revision,
				Unchanged:       true,
				AddedCount:      0,
				Shots:           shotResults,
			}
			return nil
		}

		refsToInsert := []model.ShotAssetReference{}
		affectedShotIDs := []string{}
		for _, shotID := range shotOrder {
			added := addedByShot[shotID]
			for _, vid := range added {
				refsToInsert = append(refsToInsert, model.ShotAssetReference{
					ID:             newID(),
					ShotID:         shotID,
					AssetVersionID: vid,
					Role:           "reference",
					Status:         "linked",
				})
			}
			if len(added) > 0 {
				affectedShotIDs = append(affectedShotIDs, shotID)
			}
		}

		now := time.Now().UTC()
		updated, err := tx.ApplyStoryboardShotAssetBindingsAppend(projectID, unitID, currentProject.Revision, refsToInsert, affectedShotIDs, now)
		if err != nil {
			return err
		}
		if !updated {
			return StoryboardAssetBindingConflict("项目版本已变化，请重新预览后再确认")
		}
		postSnapshot, err := tx.ReadStoryboardAssetSnapshot(projectID, unitID, sortedShotIDs)
		if err != nil {
			return err
		}
		updatedProject := currentProject
		updatedProject.Revision = currentProject.Revision + 1
		postPreview, err := buildStoryboardAssetPreview(currentProject.UserID, updatedProject, unitID, sortedShotIDs, postSnapshot)
		if err != nil {
			return err
		}

		shotResults := make([]BindingShotAppendResult, len(shotOrder))
		for i, shotID := range shotOrder {
			added := addedByShot[shotID]
			shotResults[i] = BindingShotAppendResult{
				ShotID:          shotID,
				AddedCount:      len(added),
				AddedVersionIDs: added,
				Unchanged:       len(added) == 0,
			}
		}
		populateBindingShotAppendResults(shotResults, postPreview.Baseline.Shots)
		result = BindingAppendResult{
			ProjectRevision: updatedProject.Revision,
			Unchanged:       false,
			AddedCount:      totalIntendedAdded,
			Shots:           shotResults,
		}
		return nil
	})
	if err != nil {
		return BindingAppendResult{}, mapStoryboardBindingAppendError(err)
	}
	return result, nil
}

func (s *Service) PreviewProjectUnitShotAssetBindings(ctx context.Context, userID, projectID, unitID string, req BindingPreviewRequest) (BindingPreview, error) {
	result := BindingPreview{}
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range req.ShotIDs {
		if strings.TrimSpace(id) == "" {
			return result, BadAuthRequest("镜头标识不能为空")
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if userID == "" || projectID == "" || unitID == "" || len(ids) == 0 || len(ids) > 100 {
		return result, BadAuthRequest("请选择同章节的 1 至 100 个镜头")
	}
	sort.Strings(ids)
	err := s.repo.WithStoryboardAssetSnapshot(ctx, func(tx *repository.Repository) error {
		project, err := tx.ProjectForUser(userID, projectID)
		if err != nil {
			return err
		}
		if _, err := tx.ProjectUnit(projectID, unitID); err != nil {
			return err
		}
		snapshot, err := tx.ReadStoryboardAssetSnapshot(projectID, unitID, ids)
		if err != nil {
			return err
		}
		result, err = buildStoryboardAssetPreview(userID, *project, unitID, ids, snapshot)
		return err
	})
	if err != nil {
		return BindingPreview{}, err
	}
	return result, nil
}

// Hash normalized database values only; fingerprints are comparison tokens, not
// authorization. JSON object keys are canonicalized without losing number precision.
func storyboardBindingFingerprint(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	} // All callers pass concrete database/DTO values.
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
func storyboardBindingJSON(raw string) string {
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if !json.Valid([]byte(raw)) || decoder.Decode(&value) != nil {
		return raw
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
func storyboardBindingStatus(status model.AssetVersionStatus) bool {
	return status == model.AssetVersionStatusDraft || status == model.AssetVersionStatusReview || status == model.AssetVersionStatusConfirmed
}

func buildStoryboardAssetPreview(userID string, project model.Project, unitID string, ids []string, snapshot repository.StoryboardAssetSnapshot) (BindingPreview, error) {
	result := BindingPreview{Baseline: BindingBaseline{ProjectID: project.ID, UnitID: unitID, ProjectRevision: project.Revision, ShotIDs: ids, Shots: []BindingShotBaseline{}, Catalog: []BindingCatalogBaseline{}}, Shots: []BindingShotPreview{}}
	links := map[string][]model.ProjectAssetLink{}
	for _, link := range snapshot.Links {
		links[link.AssetID] = append(links[link.AssetID], link)
	}
	resources := map[string]model.Resource{}
	for _, resource := range snapshot.Resources {
		resources[resource.ID] = resource
	}
	versions := map[string][]model.AssetVersion{}
	for _, version := range snapshot.Versions {
		versions[version.AssetID] = append(versions[version.AssetID], version)
	}
	representations := map[string][]model.AssetRepresentation{}
	for _, rep := range snapshot.Representations {
		rep.MetadataJSON = storyboardBindingJSON(rep.MetadataJSON)
		representations[rep.AssetVersionID] = append(representations[rep.AssetVersionID], rep)
	}
	// Keep orphaned versions and dangling links visible without granting identity
	// or bindability to a missing asset row.
	present := map[string]bool{}
	assets := append([]model.Asset{}, snapshot.Assets...)
	for _, asset := range assets {
		present[asset.ID] = true
	}
	missing := map[string]bool{}
	for id := range versions {
		if !present[id] {
			missing[id] = true
		}
	}
	for id := range links {
		if !present[id] {
			missing[id] = true
		}
	}
	for id := range missing {
		assets = append(assets, model.Asset{ID: id})
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].ID < assets[j].ID })
	catalog := storyboardAssetCatalog{ProjectID: project.ID, Assets: []storyboardAssetSnapshot{}}
	for _, asset := range assets {
		asset.PayloadJSON = storyboardBindingJSON(asset.PayloadJSON)
		entry := BindingCatalogBaseline{AssetID: asset.ID, AssetFingerprint: storyboardBindingFingerprint(asset), LinkFingerprint: storyboardBindingFingerprint(links[asset.ID]), Versions: []BindingVersionBaseline{}}
		if !present[asset.ID] {
			entry.AssetFingerprint = storyboardBindingFingerprint(nil)
		}
		images := map[string]bool{}
		for _, version := range versions[asset.ID] {
			version.DefinitionJSON = storyboardBindingJSON(version.DefinitionJSON)
			reps := representations[version.ID]
			if reps == nil {
				reps = []model.AssetRepresentation{}
			}
			resourceIDs := map[string]bool{}
			imageAvailable := func(id string) bool {
				resource, ok := resources[id]
				return ok && resource.UserID == asset.UserID && resource.Kind == "image" && resource.Status == model.ResourceStatusReady && resource.Width > 0 && resource.Height > 0 && strings.HasPrefix(resource.MimeType, "image/")
			}
			for _, rep := range reps {
				if rep.ResourceID != "" {
					resourceIDs[rep.ResourceID] = true
				}
				if rep.MediaType == "image" && imageAvailable(rep.ResourceID) {
					images[version.ID] = true
				}
			}
			reference := referenceImageFromDefinition(version.DefinitionJSON)
			if reference != nil && reference.ResourceID != "" {
				resourceIDs[reference.ResourceID] = true
				if imageAvailable(reference.ResourceID) {
					images[version.ID] = true
				}
			}
			type mediaResource struct {
				ID               string
				Exists           bool
				Resource         model.Resource
				StorageSettingID string
				UploadKey        *string
			}
			media := []mediaResource{}
			resourceOrder := []string{}
			for id := range resourceIDs {
				resourceOrder = append(resourceOrder, id)
			}
			sort.Strings(resourceOrder)
			for _, id := range resourceOrder {
				resource, ok := resources[id]
				media = append(media, mediaResource{id, ok, resource, resource.StorageSettingID, resource.UploadKey})
			}
			mediaFingerprint := storyboardBindingFingerprint(struct {
				Representations []model.AssetRepresentation
				Reference       *ProjectReferenceImage
				Resources       []mediaResource
			}{reps, reference, media})
			bindable := asset.UserID == userID && len(links[asset.ID]) > 0 && storyboardBindingStatus(asset.Status) && storyboardBindingStatus(version.Status)
			entry.Versions = append(entry.Versions, BindingVersionBaseline{version.ID, storyboardBindingFingerprint(version), mediaFingerprint, bindable, !images[version.ID]})
		}
		result.Baseline.Catalog = append(result.Baseline.Catalog, entry)
		if asset.UserID == userID && len(links[asset.ID]) > 0 && (asset.Category == "character" || asset.Category == "environment" || asset.Category == "prop") {
			catalog.Assets = append(catalog.Assets, storyboardAssetSnapshot{ProjectID: project.ID, Asset: asset, Versions: versions[asset.ID], ImageVersionIDs: images})
		}
	}
	sort.Slice(result.Baseline.Catalog, func(i, j int) bool { return result.Baseline.Catalog[i].AssetID < result.Baseline.Catalog[j].AssetID })
	revisions := map[string]model.ShotRevision{}
	for _, rev := range snapshot.Revisions {
		rev.ActionBeatsJSON = storyboardBindingJSON(rev.ActionBeatsJSON)
		revisions[rev.ID] = rev
	}
	for _, shot := range snapshot.Shots {
		revision, exists := revisions[shot.CurrentRevisionID]
		if shot.CurrentRevisionID != "" && (!exists || revision.ShotID != shot.ID) {
			return BindingPreview{}, gorm.ErrRecordNotFound
		}
		baseline := BindingShotBaseline{ShotID: shot.ID, ShotFingerprint: storyboardBindingFingerprint(shot), RevisionFingerprint: storyboardBindingFingerprint(revision), References: []BindingReference{}, Artifacts: []BindingArtifact{}}
		existing := []model.ShotAssetReference{}
		for _, ref := range snapshot.References {
			if ref.ShotID == shot.ID {
				existing = append(existing, ref)
				baseline.References = append(baseline.References, BindingReference{ref.ID, ref.AssetVersionID, ref.Role, ref.Status})
			}
		}
		for _, artifact := range snapshot.Artifacts {
			if artifact.ShotID == shot.ID {
				status, selected := artifact.Status, artifact.Selected
				artifact.Status = ""
				artifact.Selected = false
				artifact.UpdatedAt = artifact.CreatedAt
				artifact.MetadataJSON = storyboardBindingJSON(artifact.MetadataJSON)
				baseline.Artifacts = append(baseline.Artifacts, BindingArtifact{artifact.ID, storyboardBindingFingerprint(artifact), status, selected})
			}
		}
		capacity := applyStoryboardAssetCapacity(existing, extractStoryboardAssetEvidence(shot.Description, revision, catalog, nil))
		issues := append([]BindingIssue{}, capacity.Issues...)
		result.Baseline.Shots = append(result.Baseline.Shots, baseline)
		result.Shots = append(result.Shots, BindingShotPreview{shot.ID, baseline.References, capacity.Candidates, issues, capacity.ExistingVersionCount, capacity.RemainingCapacity})
	}
	return result, nil
}
