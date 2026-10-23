package scripts_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mac-cleanup/internal/protocol"
)

const fakeSdkmanager = `cat <<'OUT'
Installed packages:
  Path                                        | Version | Description                                | Location
  -------                                     | ------- | -------                                    | -------
  platforms;android-34                        | 3       | Android SDK Platform 34                    | platforms/android-34
  system-images;android-30;google_apis;x86_64 | 10      | Google APIs Intel x86_64 Atom System Image | system-images/android-30/google_apis/x86_64
  system-images;android-34;google_apis_playstore;arm64-v8a | 2 | Google Play ARM 64 v8a System Image | system-images/android-34/google_apis_playstore/arm64-v8a
OUT`

func TestAndroidImages(t *testing.T) {
	e := newEnv(t)
	sdk := filepath.Join(e.Home, "Library/Android/sdk")
	sdkmanager := filepath.Join(sdk, "cmdline-tools/latest/bin/sdkmanager")
	e.exe(sdkmanager, fakeSdkmanager)
	e.write(filepath.Join(sdk, "system-images/android-30/google_apis/x86_64/system.img"), 32)
	e.write(filepath.Join(sdk, "system-images/android-34/google_apis_playstore/arm64-v8a/system.img"), 32)
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
		if it.Size < 32768 || it.Explain == "" || len(it.Warnings) != 1 {
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
