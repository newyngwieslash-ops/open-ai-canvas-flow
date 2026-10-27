package model

import "time"

// ResourceProducerOperation is private physical accounting, never a public
// Resource status. Its immutable ID is the completion token; rows survive deletion.
type ResourceProducerOperation struct {
	ID            string `gorm:"primaryKey;size:36"`
	ResourceID    string `gorm:"index;size:36"`
	UserID        string `gorm:"size:36"`
	Variant       string `gorm:"size:24"`
	Provider      string `gorm:"size:24"`
	Endpoint      string
	Bucket        string `gorm:"size:160"`
	Namespace     string `gorm:"size:24"`
	ObjectKey     string
	Identity      string `gorm:"index;size:64"`
	Protocol      string `gorm:"size:32"`
	Fenced        bool
	PhysicalState string `gorm:"size:24"`
	Outcome       string `gorm:"size:24"`
	Proof         string `gorm:"size:48"`
	ControlOwner  string `gorm:"size:36;not null;default:''"`
	ControlPhase  string `gorm:"size:24;not null;default:''"`
	ControlResult string `gorm:"size:16;not null;default:''"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
