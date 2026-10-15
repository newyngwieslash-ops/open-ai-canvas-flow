package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestPluginBrandRefreshPreservesDisabledAndCustomManifest(t *testing.T) {
	officialDir := t.TempDir()
	t.Setenv("CANVAS_OFFICIAL_PLUGIN_DIR", officialDir)
	manifest := func(id, name, author string) []byte {
		return []byte(fmt.Sprintf(`{
			"apiVersion":"yingce.plugin/v1","id":%q,"name":%q,"author":%q,
			"version":"1.0.0","enabled":true,"documentation":"影策用户自由文案",
			"contributes":{"providers":[{"id":%q,"label":"Test Video","capabilities":["video"],"scopes":["canvas"],
			"create":{"method":"POST","path":"/tasks","fields":{"model":"request.model"}},
			"response":{"taskIdPaths":["id"],"statusPaths":["status"],"resultPaths":["video_url"],"resultKind":"video"}}]}
		}`, id, name, author, id))
	}
	const officialID = "test-official-brand"
	const customID = "test-custom-brand"
	writeOfficial := func(name, author string) {
		t.Helper()
		data := testPluginPackage(t, manifest(officialID, name, author))
		if err := os.WriteFile(filepath.Join(officialDir, "brand.yingce-plugin"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeOfficial("影策官方协议", "影策")
	dataDir := t.TempDir()
	center, err := newPluginRuntime(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	old := center.plugins[officialID]
	if old.Metadata.Name != "影策官方协议" || old.Metadata.Vendor != "影策" || old.Status != "enabled" {
		t.Fatalf("old official package was not installed: %#v", old)
	}
	if _, err := center.setEnabled(officialID, false); err != nil {
		t.Fatal(err)
	}
	stored, err := center.readRegistry()
	if err != nil {
		t.Fatal(err)
	}
	stored = append(stored, pluginRegistryRecord{ID: customID, Raw: manifest(customID, "影策用户插件", "用户署名 / 影策"), Source: PluginOriginUploaded})
	if err := center.writeRegistry(stored); err != nil {
		t.Fatal(err)
	}
	stored, err = center.readRegistry()
	if err != nil {
		t.Fatal(err)
	}
	customRaw := append([]byte(nil), stored[len(stored)-1].Raw...)
	writeOfficial("源铭振跃官方协议", "源铭振跃")
	updated, err := newPluginRuntime(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	record, ok := updated.plugins[officialID]
	if !ok || record.Metadata.ID != officialID || record.Metadata.Name != "源铭振跃官方协议" || record.Metadata.Vendor != "源铭振跃" {
		t.Fatalf("official metadata was not refreshed: %#v", record)
	}
	if record.Metadata.Enabled || record.Status != "disabled" || !record.InstalledAt.Equal(old.InstalledAt) {
		t.Fatalf("official installation state was reset: %#v", record)
	}
	if record.PackageSHA256 == "" || record.PackageSHA256 == old.PackageSHA256 {
		t.Fatal("official package cache was not refreshed")
	}
	if custom, ok := updated.plugins[customID]; !ok || custom.Status != "enabled" || !bytes.Equal(custom.Raw, customRaw) {
		t.Fatalf("custom raw manifest was changed: %#v", custom)
	}
	stored, err = updated.readRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range stored {
		if record.ID == customID {
			if !bytes.Equal(record.Raw, customRaw) {
				t.Fatal("persisted custom raw manifest was changed")
			}
			return
		}
	}
	t.Fatal("custom plugin was removed from registry")
}
