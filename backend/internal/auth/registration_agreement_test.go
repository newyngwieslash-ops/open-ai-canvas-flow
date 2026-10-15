package auth

import (
	"encoding/json"
	"testing"

	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRegistrationAgreementRecognizesSavedLegacyDefault(t *testing.T) {
	for _, brand := range []string{"源铭振跃", "星河工作室"} {
		for _, tc := range []struct{ title, want string }{
			{"影策服务协议", brand + "服务协议"}, {"《影策服务协议》", brand + "服务协议"},
			{"", brand + "服务协议"}, {"《影策创作者条款》", "影策创作者条款"},
		} {
			db := newAgreementTestDB(t)
			svc := New(repository.New(db), brandHost{name: brand}, nil)
			raw, err := json.Marshal(registrationSettingValue{Enabled: true, AgreementTitle: tc.title, AgreementContent: "影策正文\n保留所有条款"})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.SystemSetting{Key: registrationSettingKey, ValueJSON: string(raw)}).Error; err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				title, content := svc.RegistrationAgreement()
				if title != tc.want || content != "影策正文\n保留所有条款" {
					t.Errorf("brand %q saved title %q = %q / %q", brand, tc.title, title, content)
				}
				settings, err := svc.PublicAuthSettings()
				if err != nil {
					t.Fatal(err)
				}
				if settings.AgreementTitle != tc.want || settings.AgreementContent != content {
					t.Error("public auth agreement differs")
				}
				svc = New(repository.New(db), brandHost{name: brand}, nil)
			}
			var stored model.SystemSetting
			if err := db.First(&stored, "key = ?", registrationSettingKey).Error; err != nil {
				t.Fatal(err)
			}
			if stored.ValueJSON != string(raw) {
				t.Fatal("agreement GET changed JSON")
			}
		}
	}
}

func TestRegistrationAgreementFollowsBrandNameWhenUnset(t *testing.T) {
	svc := New(repository.New(newAgreementTestDB(t)), brandHost{name: "星野"}, nil)

	title, content := svc.RegistrationAgreement()
	if title != "星野服务协议" {
		t.Fatalf("default agreement title = %q", title)
	}
	if content != "" {
		t.Fatalf("default agreement content = %q", content)
	}

	settings, err := svc.PublicAuthSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.AgreementTitle != "星野服务协议" || settings.AgreementContent != "" {
		t.Fatalf("public settings agreement = %q / %q", settings.AgreementTitle, settings.AgreementContent)
	}
}

func TestUpdateRegistrationSettingStoresAgreement(t *testing.T) {
	db := newAgreementTestDB(t)
	svc := New(repository.New(db), brandHost{name: "星野"}, nil)
	if err := db.Create(&model.User{ID: "admin", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}).Error; err != nil {
		t.Fatal(err)
	}
	admin := &model.User{ID: "admin", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}

	saved, err := svc.UpdateRegistrationSetting(admin, RegistrationSettingRequest{
		Enabled:          true,
		AgreementTitle:   strPtr("《星野平台服务条款》"),
		AgreementContent: strPtr("第一条 服务说明\n\n第二条 内容合规"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Enabled || saved.AgreementTitle != "《星野平台服务条款》" {
		t.Fatalf("saved setting = %#v", saved)
	}

	reloaded, err := svc.AdminRegistrationSetting(admin)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.AgreementContent != "第一条 服务说明\n\n第二条 内容合规" {
		t.Fatalf("reloaded content = %q", reloaded.AgreementContent)
	}

	// 展示用标题去掉书名号，避免注册页出现双书名号。
	title, content := svc.RegistrationAgreement()
	if title != "星野平台服务条款" || content != "第一条 服务说明\n\n第二条 内容合规" {
		t.Fatalf("agreement = %q / %q", title, content)
	}

	settings, err := svc.PublicAuthSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.AgreementTitle != "星野平台服务条款" || settings.AgreementContent != "第一条 服务说明\n\n第二条 内容合规" {
		t.Fatalf("public settings agreement = %q / %q", settings.AgreementTitle, settings.AgreementContent)
	}
}

func TestRegistrationToggleKeepsSavedAgreement(t *testing.T) {
	db := newAgreementTestDB(t)
	svc := New(repository.New(db), brandHost{name: "星野"}, nil)
	admin := &model.User{ID: "admin", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}

	if _, err := svc.UpdateRegistrationSetting(admin, RegistrationSettingRequest{Enabled: true, AgreementContent: strPtr("条款正文")}); err != nil {
		t.Fatal(err)
	}
	enabled, err := svc.RegistrationEnabled()
	if err != nil || !enabled {
		t.Fatalf("RegistrationEnabled() = %v, %v", enabled, err)
	}

	// 管理面板切换注册开关时只提交 enabled，已保存的协议正文不能被清空。
	switched, err := svc.UpdateRegistrationSetting(admin, RegistrationSettingRequest{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if switched.Enabled || switched.AgreementContent != "条款正文" {
		t.Fatalf("toggle wiped agreement: %#v", switched)
	}

	cleared, err := svc.UpdateRegistrationSetting(admin, RegistrationSettingRequest{Enabled: true, AgreementContent: strPtr("")})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.AgreementContent != "" {
		t.Fatalf("explicit clear ignored: %#v", cleared)
	}
}

func strPtr(value string) *string { return &value }

// 配置损坏时不能再返回品牌名兜底标题：注册页会据此展示，与后台真实配置不符。
func TestRegistrationAgreementDegradesWhenSettingUnreadable(t *testing.T) {
	db := newAgreementTestDB(t)
	svc := New(repository.New(db), brandHost{name: "星野"}, nil)

	if err := db.Create(&model.SystemSetting{Key: registrationSettingKey, ValueJSON: "{not-json"}).Error; err != nil {
		t.Fatal(err)
	}

	title, content := svc.RegistrationAgreement()
	if title != "" || content != "" {
		t.Fatalf("broken setting should degrade to empty, got %q / %q", title, content)
	}

	// 错误提示仍需给出可读文案，但不能是凭空拼出来的协议名。
	if message := svc.AgreementTitleForMessage(); message != "服务协议" {
		t.Fatalf("fallback message title = %q", message)
	}
}

func newAgreementTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+kernel.NewID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.SystemSetting{}); err != nil {
		t.Fatal(err)
	}
	return db
}
