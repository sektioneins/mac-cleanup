package scripts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mac-cleanup/internal/protocol"
)

// androidSDK creates a fake sdkmanager that records every call in
// sdkmanager.calls, and returns the SDK root and the sdkmanager path.
func androidSDK(e *env) (string, string) {
	sdk := filepath.Join(e.Home, "Library/Android/sdk")
	sdkmanager := filepath.Join(sdk, "cmdline-tools/latest/bin/sdkmanager")
	e.exe(sdkmanager, `echo "$*" >> "$0.calls"`)
	return sdk, sdkmanager
}

// androidImage creates an installed image directory with the given marker
// file (package.xml or source.properties; empty for none).
func androidImage(e *env, sdk, rel, marker string) string {
	dir := filepath.Join(sdk, "system-images", rel)
	e.write(filepath.Join(dir, "system.img"), 32)
	if marker != "" {
		e.write(filepath.Join(dir, marker), 1)
	}
	return dir
}

func TestAndroidImages(t *testing.T) {
	e := newEnv(t)
	sdk, sdkmanager := androidSDK(e)
	androidImage(e, sdk, "android-30/google_apis/x86_64", "package.xml")
	androidImage(e, sdk, "android-34/google_apis_playstore/arm64-v8a", "source.properties")
	androidImage(e, sdk, "android-35/default/arm64-v8a", "")
	e.write(filepath.Join(sdk, "platforms/android-34/android.jar"), 32)
	e.running("qemu-system")
	items := e.run("android-images")
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}
	want := []struct{ id, name, rel string }{
		{"system-images;android-30;google_apis;x86_64", "Android image 30 google_apis x86_64", "system-images/android-30/google_apis/x86_64"},
		{"system-images;android-34;google_apis_playstore;arm64-v8a", "Android image 34 google_apis_playstore arm64-v8a", "system-images/android-34/google_apis_playstore/arm64-v8a"},
	}
	for i, w := range want {
		it := items[i]
		if it.ID != w.id || it.Name != w.name || it.Category != "Developer" || it.Grade != protocol.Redownload || it.Default != protocol.Delete {
			t.Errorf("unexpected item %+v", it)
		}
		if len(it.Paths) != 1 || it.Paths[0] != filepath.Join(sdk, w.rel) {
			t.Errorf("paths = %v", it.Paths)
		}
		if it.Size < 32768 || it.Explain == "" || len(it.Warnings) != 1 || it.Admin {
			t.Errorf("unexpected fields %+v", it)
		}
		wantCmd := "'" + sdkmanager + "' --uninstall '" + w.id + "'"
		if got := it.Commands[protocol.Delete]; got != wantCmd {
			t.Errorf("cmd_delete = %q, want %q", got, wantCmd)
		}
		if len(it.Commands) != 1 {
			t.Errorf("commands = %v, want only delete", it.Commands)
		}
	}
	if _, err := os.Stat(sdkmanager + ".calls"); err == nil {
		t.Error("sdkmanager was called during the scan")
	}
}

func TestAndroidNoSdkmanager(t *testing.T) {
	e := newEnv(t)
	sdk := filepath.Join(e.Home, "Library/Android/sdk")
	androidImage(e, sdk, "android-30/google_apis/x86_64", "package.xml")
	if items := e.run("android-images"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestAndroidNotWritable(t *testing.T) {
	e := newEnv(t)
	sdk, _ := androidSDK(e)
	dir := androidImage(e, sdk, "android-30/google_apis/x86_64", "package.xml")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	items := e.run("android-images")
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	want := dir + " is not writable by you"
	if it := items[0]; it.Admin || len(it.Warnings) != 1 || it.Warnings[0] != want {
		t.Errorf("admin %v, warnings %q, want [%q]", it.Admin, it.Warnings, want)
	}
}

func TestAndroidUnreadable(t *testing.T) {
	e := newEnv(t)
	sdk, _ := androidSDK(e)
	androidImage(e, sdk, "android-30/google_apis/x86_64", "package.xml")
	images := filepath.Join(sdk, "system-images")
	if err := os.Chmod(images, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(images, 0o755) })
	stdout, stderr := e.runFail("android-images")
	if stdout != "" || !strings.Contains(stderr, images) {
		t.Errorf("stdout %q, stderr %q", stdout, stderr)
	}
}

func TestAndroidAbsent(t *testing.T) {
	e := newEnv(t)
	if items := e.run("android-images"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

func TestJavaVersions(t *testing.T) {
	e := newEnv(t)
	base := filepath.Join(e.Root, "Library/Java/JavaVirtualMachines")
	e.write(filepath.Join(base, "temurin-17.jdk/Contents/Home/bin/java"), 32)
	e.write(filepath.Join(base, "zulu-21.jdk/Contents/Home/bin/java"), 32)
	e.tool("java", "exit 1")
	items := e.run("java-versions")
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	for i, id := range []string{"temurin-17", "zulu-21"} {
		it := items[i]
		dir := filepath.Join(base, id+".jdk")
		if it.ID != id || it.Name != id || it.Category != "Developer" || it.Grade != protocol.MayNeed || it.Default != protocol.Trash {
			t.Errorf("unexpected item %+v", it)
		}
		if len(it.Paths) != 1 || it.Paths[0] != dir || it.Size < 32768 {
			t.Errorf("paths/size = %v %d", it.Paths, it.Size)
		}
		if !strings.Contains(it.Explain, id+".jdk") || !strings.Contains(it.Explain, "fail") {
			t.Errorf("explain = %q", it.Explain)
		}
		if got, want := it.Commands[protocol.Delete], "rm -rf -- '"+dir+"'"; got != want && !it.Admin {
			t.Errorf("cmd_delete = %q", got)
		}
	}
}

func TestJavaAbsent(t *testing.T) {
	e := newEnv(t)
	if items := e.run("java-versions"); len(items) != 0 {
		t.Fatalf("got %d items, want 0", len(items))
	}
}

const javaActiveSentence = "This is the Java version the java command currently uses."

func twoJDKs(e *env) string {
	base := filepath.Join(e.Root, "Library/Java/JavaVirtualMachines")
	e.write(filepath.Join(base, "a.jdk/Contents/Home/bin/java"), 32)
	e.write(filepath.Join(base, "b.jdk/Contents/Home/bin/java"), 32)
	return base
}

func TestJavaActive(t *testing.T) {
	e := newEnv(t)
	base := twoJDKs(e)
	e.tool("java", "echo '    java.home = "+base+"/b.jdk/Contents/Home' >&2")
	items := e.run("java-versions")
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	a, b := items[0], items[1]
	if a.ID != "a" || a.Name != "a" || strings.Contains(a.Explain, javaActiveSentence) {
		t.Errorf("a should be unchanged: %+v", a)
	}
	if b.ID != "b" || b.Name != "b (active)" || !strings.Contains(b.Explain, javaActiveSentence) {
		t.Errorf("b should be active: %+v", b)
	}
	if b.Grade != protocol.MayNeed || b.Default != protocol.Trash {
		t.Errorf("active item changed grade/default: %+v", b)
	}
}

func TestJavaNoJavaCommand(t *testing.T) {
	e := newEnv(t)
	twoJDKs(e)
	e.tool("java", "exit 1")
	items := e.run("java-versions")
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	for _, it := range items {
		if strings.Contains(it.Name, "(active)") || strings.Contains(it.Explain, javaActiveSentence) {
			t.Errorf("unexpected active item %+v", it)
		}
	}
}

func TestJavaNotRunWithoutJDK(t *testing.T) {
	e := newEnv(t)
	marker := filepath.Join(e.Root, "java-ran")
	e.tool("java", "touch '"+marker+"'")
	e.run("java-versions")
	if _, err := os.Stat(marker); err == nil {
		t.Error("java was run although no JDK exists")
	}
}

func TestJavaScanDisablesPerfData(t *testing.T) {
	e := newEnv(t)
	twoJDKs(e)
	record := filepath.Join(e.Bin, "java.args")
	e.tool("java", `echo "$*" >> '`+record+`'`)
	e.run("java-versions")
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if !strings.Contains(" "+line+" ", " -XX:-UsePerfData ") {
			t.Errorf("java called without -XX:-UsePerfData: %q", line)
		}
	}
}
