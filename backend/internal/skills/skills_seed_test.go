package skills

import (
	"path/filepath"
	"regexp"
	"testing"
	"testing/fstest"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDoubaoAudioBuiltinUsesSupportedCategory(t *testing.T) {
	packages, err := loadBuiltinSkillPackages(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range packages {
		if item.skill.ID == "doubao-audio-prompt" {
			if item.skill.Tag != "creative" {
				t.Fatalf("doubao audio category = %q, want creative", item.skill.Tag)
			}
			return
		}
	}
	t.Fatal("doubao-audio-prompt builtin is missing")
}

// 总纲名录里写死的 `cards/…` 路径会被 Agent 直接交给 skill_read_file；
// 包里缺卡时只会在运行时报「读取参考资料失败」，所以在这里提前拦住。
func TestBuiltinPackagesShipReferencedCards(t *testing.T) {
	packages, err := loadBuiltinSkillPackages(nil)
	if err != nil {
		t.Fatal(err)
	}
	// 只认具体文件名；`cards/<slug>.md` 这类占位写法不算引用。
	reference := regexp.MustCompile("`(cards/[^`<>\\s]+\\.md)`")
	for _, item := range packages {
		for _, match := range reference.FindAllStringSubmatch(string(item.archive.Files["SKILL.md"]), -1) {
			if _, ok := item.archive.Files[match[1]]; !ok {
				t.Errorf("builtin skill %s (%s) references missing %s", item.skill.ID, item.skill.Name, match[1])
			}
		}
	}
}

func TestBuiltinMarkdownPackagesPreserveHistoryAndUserState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "skills.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Skill{}, &model.UserSkillState{}, &model.SkillVersion{}, &model.SkillFile{}, &model.BuiltinSkillTombstone{}, &model.User{}, &model.UserIdentity{}); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), t.TempDir(), nil)
	packages, err := loadBuiltinSkillPackages(nil)
	if err != nil {
		t.Fatal(err)
	}
	const historyID = "yingce-short-drama-workflow"
	const oldBody = "---\nname: 影策短剧制作工作流\ndescription: 旧官方工作流\nmetadata:\n  skillId: yingce-short-drama-workflow\n  author: 影策\n  version: 1.0.0\n---\n旧版正文\n"
	oldPackages, err := loadBuiltinSkillPackagesFromFS(fstest.MapFS{
		"影策短剧制作工作流/SKILL.md": &fstest.MapFile{Data: []byte(oldBody)},
	}, svc.repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(oldPackages) != 1 {
		t.Fatalf("old packages = %d, want 1", len(oldPackages))
	}
	old := oldPackages[0]
	if err := svc.repo.UpsertBuiltinSkills([]model.Skill{old.skill}); err != nil {
		t.Fatal(err)
	}
	if err := svc.addSkillArchiveVersion(&old.skill, old.archive, "builtin", "", "", "", "", false); err != nil {
		t.Fatal(err)
	}
	state := model.UserSkillState{ID: "test-state", UserID: "test-user", SkillID: historyID, Added: true, Liked: true, InstalledVersionID: old.skill.CurrentVersionID, AutoUpdate: false}
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := svc.EnsureBuiltinSkills(); err != nil {
			t.Fatal(err)
		}
	}
	var count int64
	if err := db.Model(&model.Skill{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != int64(len(packages)) {
		t.Fatalf("builtin skill count = %d, want %d", count, len(packages))
	}
	var saved model.UserSkillState
	if err := db.First(&saved, "id = ?", state.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !saved.Added || !saved.Liked || saved.AutoUpdate || saved.InstalledVersionID != state.InstalledVersionID || saved.UserID != state.UserID || saved.SkillID != historyID {
		t.Fatalf("user state lost: %#v", saved)
	}
	var skill model.Skill
	if err := db.First(&skill, "id = ?", historyID).Error; err != nil {
		t.Fatal(err)
	}
	if skill.Name != "源铭振跃短剧制作工作流" || skill.AuthorName != "源铭振跃" {
		t.Fatalf("official metadata was not refreshed: name=%q author=%q", skill.Name, skill.AuthorName)
	}
	if skill.CurrentVersionID == "" || skill.CurrentVersionID == state.InstalledVersionID || skill.FileCount < 1 {
		t.Fatalf("history skill package not initialized: %#v", skill)
	}
	assertSkillVersionCount(t, db, historyID, 2)
	pinned, err := svc.repo.SkillVersion(saved.InstalledVersionID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := svc.readSkillArchiveEntry(pinned, "SKILL.md")
	if err != nil || string(body) != oldBody {
		t.Fatalf("pinned package was changed: body=%q err=%v", body, err)
	}
}

func TestBuiltinSkillPackageMetadataParser(t *testing.T) {
	body := []byte("---\nname: 测试\ndescription: 描述\nmetadata:\n  version: \"1.0.0\"\n  tag: drama\n---\n\n正文\n")
	metadata, err := parseBuiltinSkillMetadata(body)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Name != "测试" || metadata.Description != "描述" || metadata.Version != "1.0.0" || metadata.Tag != "drama" {
		t.Fatalf("metadata = %#v", metadata)
	}
	if _, err := parseBuiltinSkillMetadata([]byte("# no frontmatter")); err == nil {
		t.Fatal("expected missing frontmatter error")
	}
}

func TestBuiltinAuthorFallbackPreservesExplicitAuthor(t *testing.T) {
	for _, tc := range []struct{ author, want string }{
		{"", "源铭振跃"},
		{"OpenAI", "OpenAI"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			files := fstest.MapFS{
				"中文目录/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: 测试\ndescription: 描述\nmetadata:\n  skillId: stable-skill\n  author: " + tc.author + "\n---\n正文\n")},
			}
			packages, err := loadBuiltinSkillPackagesFromFS(files, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(packages) != 1 {
				t.Fatalf("loaded %d packages, want 1", len(packages))
			}
			skill := packages[0].skill
			if skill.AuthorName != tc.want || skill.OwnerID != "yingce-system" || skill.ID != "stable-skill" {
				t.Fatalf("author=%q owner=%q id=%q", skill.AuthorName, skill.OwnerID, skill.ID)
			}
		})
	}
}

func TestBuiltinShortDramaReadableDirectoryKeepsStableIdentity(t *testing.T) {
	packages, err := loadBuiltinSkillPackages(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range packages {
		if item.skill.MarkdownURL != "源铭振跃短剧制作工作流/SKILL.md" {
			continue
		}
		if item.skill.ID != "yingce-short-drama-workflow" || item.skill.OwnerID != "yingce-system" {
			t.Fatalf("renamed package changed stable identity: id=%q owner=%q", item.skill.ID, item.skill.OwnerID)
		}
		if item.skill.Name != "源铭振跃短剧制作工作流" || item.skill.AuthorName != "源铭振跃" {
			t.Fatalf("loaded name=%q author=%q", item.skill.Name, item.skill.AuthorName)
		}
		if len(item.archive.Files["references/canvas-execution.md"]) == 0 {
			t.Fatal("renamed embedded package lost its execution reference")
		}
		return
	}
	t.Fatal("renamed short-drama directory was not loaded from embedded files")
}
