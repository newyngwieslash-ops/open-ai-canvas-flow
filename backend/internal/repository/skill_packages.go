package repository

import (
	"yingce/backend/internal/model"

	"gorm.io/gorm"
)

func (r *Repository) CreateSkillWithPackage(skill *model.Skill, version *model.SkillVersion, files []model.SkillFile, ownerState *model.UserSkillState) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(skill).Error; err != nil {
			return err
		}
		if err := tx.Create(version).Error; err != nil {
			return err
		}
		if len(files) > 0 {
			if err := tx.Create(&files).Error; err != nil {
				return err
			}
		}
		return tx.Create(ownerState).Error
	})
}

func (r *Repository) AddSkillVersion(skill *model.Skill, version *model.SkillVersion, files []model.SkillFile) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(version).Error; err != nil {
			return err
		}
		if len(files) > 0 {
			if err := tx.Create(&files).Error; err != nil {
				return err
			}
		}
		return tx.Save(skill).Error
	})
}

func (r *Repository) SkillVersion(id string) (*model.SkillVersion, error) {
	var version model.SkillVersion
	if err := r.db.First(&version, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &version, nil
}

func (r *Repository) SkillFiles(versionID string) ([]model.SkillFile, error) {
	var files []model.SkillFile
	err := r.db.Where("skill_version_id = ?", versionID).Order("path asc").Find(&files).Error
	return files, err
}

func (r *Repository) SkillsForPackageEnsure() ([]model.Skill, error) {
	var skills []model.Skill
	err := r.db.Where("status = ?", 1).Find(&skills).Error
	return skills, err
}

func (r *Repository) AutoUpdatingGitHubSkills() ([]model.Skill, error) {
	var skills []model.Skill
	err := r.db.Where("status = ? AND source_type = ? AND auto_update = ?", 1, "github", true).Find(&skills).Error
	return skills, err
}

func (r *Repository) SaveSkill(skill *model.Skill) error {
	return r.db.Save(skill).Error
}

func (r *Repository) PruneOldSkillVersions(skillID string, currentVersionID string, keepCount int) ([]string, error) {
	if keepCount < 1 {
		keepCount = 1
	}
	var prunedPackageKeys []string
	err := r.db.Transaction(func(tx *gorm.DB) error {
		var versions []model.SkillVersion
		if err := tx.Where("skill_id = ?", skillID).Order("created_at desc, id desc").Find(&versions).Error; err != nil {
			return err
		}
		if len(versions) <= keepCount {
			return nil
		}
		var referencedIDs []string
		if err := tx.Model(&model.UserSkillState{}).
			Where("skill_id = ? AND installed_version_id <> ''", skillID).
			Pluck("installed_version_id", &referencedIDs).Error; err != nil {
			return err
		}
		protected := make(map[string]struct{}, len(referencedIDs)+1)
		if currentVersionID != "" {
			protected[currentVersionID] = struct{}{}
		}
		for _, id := range referencedIDs {
			protected[id] = struct{}{}
		}
		for _, candidate := range versions[keepCount:] {
			if _, ok := protected[candidate.ID]; ok {
				continue
			}
			if err := tx.Where("skill_version_id = ?", candidate.ID).Delete(&model.SkillFile{}).Error; err != nil {
				return err
			}
			if err := tx.Where("id = ?", candidate.ID).Delete(&model.SkillVersion{}).Error; err != nil {
				return err
			}
			if candidate.PackageKey != "" {
				prunedPackageKeys = append(prunedPackageKeys, candidate.PackageKey)
			}
		}
		return nil
	})
	return prunedPackageKeys, err
}
