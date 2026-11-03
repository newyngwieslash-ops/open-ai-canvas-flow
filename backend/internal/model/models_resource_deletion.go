package model

import "time"

const ResourceStatusDeleting ResourceStatus = "deleting"
const AssetVersionStatusDeleting AssetVersionStatus = "deleting"

// ResourceDeletionIntent retains the complete business deletion set until all
// required objects have succeeded. Completed intents are tombstones, not references.
type ResourceDeletionIntent struct {
	ID              string `gorm:"primaryKey;size:36"`
	UserID          string `gorm:"index;size:36"`
	AssetIDsJSON    string `gorm:"type:text"`
	ResourceIDsJSON string `gorm:"type:text"`
	Status          string `gorm:"index;size:24"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ResourceDeletionObject shares its ID with its outbox job. Frozen identity and
// completion survive job removal; no storage credentials are persisted here.
type ResourceDeletionObject struct {
	ID              string `gorm:"primaryKey;size:36"`
	IntentID        string `gorm:"index;size:36"`
	ResourceIDsJSON string `gorm:"type:text"`
	Variant         string `gorm:"size:24"`
	Provider        string `gorm:"size:24"`
	Endpoint        string
	Bucket          string `gorm:"size:160"`
	ObjectKey       string `gorm:"index"`
	Status          string `gorm:"index;size:24"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
