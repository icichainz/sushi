package opener

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/icichainz/sushi/internal/jxa"
)

// App is an application that can open a file, as Finder's Open With menu
// lists it
type App struct {
	Name    string // The bundle's name without .app, as in "TextEdit"
	Path    string // The bundle, as in /System/Applications/TextEdit.app
	Default bool   // Whether it is the one double-clicking uses
}

// Apps lists the apps that can open path, the default one first, as macOS
// Launch Services knows them (through NSWorkspace, which needs macOS 12).
// The same app can be installed in more than one place, each listed.
func Apps(ctx context.Context, run jxa.Runner, path string) ([]App, error) {
	script, err := appsScript(path)
	if err != nil {
		return nil, err
	}
	out, err := run(ctx, script)
	if err != nil {
		return nil, err
	}
	var got struct {
		Default string   `json:"default"`
		Apps    []string `json:"apps"`
	}
	if err := jxa.Decode(out, &got); err != nil {
		return nil, err
	}
	return appList(got.Default, got.Apps), nil
}

// appList names the apps, putting the default first, once
func appList(def string, paths []string) []App {
	var apps []App
	if def != "" {
		apps = append(apps, App{Name: appName(def), Path: def, Default: true})
	}
	for _, p := range paths {
		if p != def && p != "" {
			apps = append(apps, App{Name: appName(p), Path: p})
		}
	}
	return apps
}

// appName is an app bundle's name without its extension
func appName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".app")
}

// appsScript builds the script that asks NSWorkspace for the apps
func appsScript(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%s is not an absolute path", path)
	}
	lit, err := jxa.Literal(path)
	if err != nil {
		return "", err
	}
	return jxa.Prelude + `function run() {
	const ws = $.NSWorkspace.sharedWorkspace;
	const url = $.NSURL.fileURLWithPath(` + lit + `);
	const def = ws.URLForApplicationToOpenURL(url);
	const all = ws.URLsForApplicationsToOpenURL(url);
	const apps = [];
	const n = (all && !all.isNil()) ? Number(all.count) : 0;
	for (let i = 0; i < n; i++) {
		apps.push(ObjC.unwrap(all.objectAtIndex(i).path));
	}
	return result({default: (def && !def.isNil()) ? ObjC.unwrap(def.path) : '', apps: apps});
}
`, nil
}

// OpenWithArgs returns the arguments of open(1) that open paths with the
// app at app, which open takes by its path as well as by its name
func OpenWithArgs(app string, paths ...string) []string {
	return append([]string{"-a", app}, paths...)
}

// Reveal shows paths selected in Finder, in a window of their folder, and
// brings Finder to the front. One path is better done by "open -R"; this
// selects any number at once. NSWorkspace asks Finder itself, so unlike
// scripting Finder, it needs no permission to control other apps.
func Reveal(ctx context.Context, run jxa.Runner, paths []string) error {
	script, err := revealScript(paths)
	if err != nil {
		return err
	}
	_, err = run(ctx, script)
	return err
}

// revealScript builds the script that selects paths in Finder
func revealScript(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("nothing to show in Finder")
	}
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			return "", fmt.Errorf("%s is not an absolute path", p)
		}
	}
	list, err := jxa.Literal(paths)
	if err != nil {
		return "", err
	}
	return jxa.Prelude + `function run() {
	const paths = ` + list + `;
	const urls = $.NSMutableArray.array;
	paths.forEach(p => urls.addObject($.NSURL.fileURLWithPath(p)));
	$.NSWorkspace.sharedWorkspace.activateFileViewerSelectingURLs(urls);
	return result({});
}
`, nil
}

// RevealArgs returns the arguments of open(1) that show path selected in
// Finder
func RevealArgs(path string) []string {
	return []string{"-R", path}
}
