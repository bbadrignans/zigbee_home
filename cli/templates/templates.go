package templates

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"text/template"
	"time"

	"github.com/ffenix113/zigbee_home/cli/config"
	"github.com/ffenix113/zigbee_home/cli/types/generator"
	"github.com/ffenix113/zigbee_home/cli/types/sensor"
	"github.com/ffenix113/zigbee_home/cli/zcl/cluster"
)

//go:embed src/*.tpl src/*/*.tpl src/*/*/*.tpl src/*/*/*/*.tpl
var TemplateFS embed.FS

var knownClusterAttrTemplates = map[cluster.ID]string{
	cluster.ID_DEVICE_TEMP_CONFIG:       "define_device_temp_config_attr_list",
	cluster.ID_ON_OFF:                   "define_on_off_attr_list",
	cluster.ID_TEMP_MEASUREMENT:         "define_temperature_attr_list",
	cluster.ID_REL_HUMIDITY_MEASUREMENT: "define_humidity_attr_list",
	cluster.ID_PRESSURE_MEASUREMENT:     "define_pressure_attr_list",
}

type Templates struct {
	templates    *template.Template
	templateTree templateTree
}

type templateTree struct {
	tree map[string]*templateTree
	tpl  *template.Template
}

type SensorCtx struct {
	Endpoint int
	Sensor   sensor.Sensor
	Device   *config.Device
	Extender generator.Extender
}

type ClusterCtx struct {
	Endpoint int
	Cluster  cluster.Cluster
}

type Context struct {
	GeneratedOn time.Time
	Version     string
	// Each sensor will have it's own endpoint,
	// which should be okay for now.
	// Packing multiple sensor into one endpoint
	// can be done in the future.
	Device *config.Device

	Extenders []generator.Extender
}

func NewTemplates(templateFS fs.FS) *Templates {
	t := &Templates{
		templateTree: templateTree{
			tree: make(map[string]*templateTree),
		},
	}

	t.templates = template.Must(template.New("").Parse(""))
	t.templates.Funcs(template.FuncMap{
		"clusterToAttrTemplate": t.clusterToAttrTemplate,
		"render":                t.render,
		"maybeRender":           t.maybeRender,
		"maybeRenderExtender":   t.maybeRenderExtender,
		"sensorCtx":             sensorCtx,
		"clusterCtx":            clusterCtx,
		"isLast":                isLast,
		"sum":                   sum,
		"joinPath": func(strs ...string) string {
			return strings.Join(strs, "/")
		},
	})

	must(t.parseByDir(templateFS, path.Join("src", "extenders", "*.tpl"), nil))
	must(t.parseByDir(templateFS, path.Join("src", "extenders", "*", "*.tpl"), nil))
	must(t.parseByDir(templateFS, path.Join("src", "extenders", "*", "*", "*.tpl"), nil))

	t.templates = template.Must(t.templates.ParseFS(templateFS, path.Join("src", "*.tpl"), path.Join("src", "zigbee", "*.tpl")))

	return t
}

func (t *Templates) parseByDir(tplFS fs.FS, pattern string, validateTpl func(t *template.Template) error) error {
	files, err := fs.Glob(tplFS, pattern)
	if err != nil {
		return fmt.Errorf("glob template fs: %w", err)
	}

	for _, tplFile := range files {
		openTpl, err := tplFS.Open(tplFile)
		if err != nil {
			return fmt.Errorf("open template %q: %w", tplFile, err)
		}
		defer openTpl.Close()

		newTpl := templateFromPath(&t.templateTree, t.templates, tplFile)

		tplText, err := io.ReadAll(openTpl)
		if err != nil {
			return fmt.Errorf("read template %q: %w", tplFile, err)
		}

		if _, err := newTpl.Parse(string(tplText)); err != nil {
			return fmt.Errorf("parse template %q: %w", tplFile, err)
		}

		if validateTpl != nil {
			if err := validateTpl(newTpl); err != nil {
				return fmt.Errorf("validate template %q: %w", tplFile, err)
			}
		}
	}

	return nil
}

func templateFromPath(root *templateTree, baseTpl *template.Template, tplPath string) *template.Template {
	const templateExtention = ".tpl"

	pathParts := strings.Split(tplPath, string(os.PathSeparator))

	tree := root
	for _, pathPart := range pathParts[:len(pathParts)-1] {
		if tree.tree == nil {
			tree.tree = make(map[string]*templateTree, 1)
		}

		subTree, ok := tree.tree[pathPart]
		if !ok {
			subTree = &templateTree{
				tree: make(map[string]*templateTree),
			}
			tree.tree[pathPart] = subTree
		}
		tree = subTree
	}

	templateName := pathParts[len(pathParts)-1]

	pathPart, _ := strings.CutSuffix(templateName, templateExtention)

	subTree, ok := tree.tree[pathPart]
	if !ok {
		subTree = &templateTree{
			tree: make(map[string]*templateTree),
		}
		tree.tree[pathPart] = subTree
	}
	tree = subTree

	tree.tpl, _ = baseTpl.Clone()
	tree.tpl = tree.tpl.New(pathPart)

	return tree.tpl
}

func (t *Templates) WriteTo(srcDir string, device *config.Device, extenders []generator.Extender) error {
	ctx := Context{
		GeneratedOn: time.Now().UTC(),
		Version:     "0.0.0-dev",
		Device:      device,

		Extenders: extenders,
	}

	for _, sourceDefinition := range sourceFiles {
		template := t.templates.Lookup(sourceDefinition[1])
		if template == nil {
			return fmt.Errorf("tried to write unknown template: %q", sourceDefinition[1])
		}

		if err := writeTemplate(template, path.Join(srcDir, sourceDefinition[0]), ctx); err != nil {
			return fmt.Errorf("write template: %w", err)
		}
	}

	for _, extender := range extenders {
		if err := t.verifyExtender(extender); err != nil {
			return fmt.Errorf("extender %q is invalid: %w", generator.ExtenderName(extender), err)
		}

		for _, fileToWrite := range extender.WriteFiles() {
			template := t.findExtendedTemplate(fileToWrite.TemplateName)
			if err := writeTemplate(template, path.Join(srcDir, fileToWrite.FileName), ctx); err != nil {
				return fmt.Errorf("write extender file %q: %w", fileToWrite.FileName, err)
			}
		}
	}

	return nil
}

var sourceFiles = [][2]string{
	{"../CMakeLists.txt", "CMakeLists.txt.tpl"},
	{"main.c", "main.c.tpl"},
	{"device.h", "device.h.tpl"},
	{"clusters.h", "clusters.h.tpl"},
}

var knownExtenders = [...]string{
	"include",
	"top_level",
	"main",
	"loop",
}

func (t *Templates) verifyExtender(extender generator.Extender) error {
	if extender.Template() == "" {
		return nil
	}

	tpl := t.findExtendedTemplate(extender.Template())
	if tpl == nil {
		return fmt.Errorf("required extention template not found: %q", extender.Template())
	}

	var foundExtenders int
	for _, knownExtender := range knownExtenders {
		extdTpl := tpl.Lookup(knownExtender)
		if extdTpl != nil {
			foundExtenders++
		}
	}

	if foundExtenders != len(tpl.Templates())-2 {
		return fmt.Errorf("extender template has weird templates: %s", tpl.DefinedTemplates())
	}

	return nil
}

func writeTemplate(template *template.Template, filePath string, ctx any) error {
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("create %q: %w", filePath, err)
	}

	defer file.Close()

	if err := template.Execute(file, ctx); err != nil {
		return fmt.Errorf("execute source template %q: %w", template.Name(), err)
	}

	return nil
}

func (t *Templates) findExtendedTemplate(templateName string) *template.Template {
	nameParts := strings.Split("src/extenders/"+templateName, "/")

	tree := &t.templateTree
	for _, namePart := range nameParts {
		tree = tree.tree[namePart]
		if tree == nil {
			return nil
		}
	}

	return tree.tpl
}

func (t *Templates) clusterToAttrTemplate(cluster cluster.Cluster) (string, error) {
	tplName, ok := knownClusterAttrTemplates[cluster.ID()]
	if !ok {
		return "", fmt.Errorf("unknown cluster ID: %q", cluster.ID().String())
	}

	return tplName, nil
}

func (t *Templates) render(tplName string, ctx any) (string, error) {
	var buf bytes.Buffer

	if err := t.templates.ExecuteTemplate(&buf, tplName, ctx); err != nil {
		return "", fmt.Errorf("execute %q: %w", tplName, err)
	}

	return buf.String(), nil
}

// maybeRender will render template `tplName`, if it exists.
// This is useful for optional templates.
func (t *Templates) maybeRender(tplName string, ctx any) (string, error) {

	tpl := t.templates.Lookup(tplName)
	if tpl == nil {
		return "", nil
	}

	var buf bytes.Buffer
	if err := t.templates.ExecuteTemplate(&buf, tplName, ctx); err != nil {
		return "", fmt.Errorf("execute %q: %w", tplName, err)
	}

	return buf.String(), nil
}

// maybeRender will render template `tplName`, if it exists.
// This is useful for optional templates.
func (t *Templates) maybeRenderExtender(tplPath, tplName string, ctx any) (string, error) {
	if tplPath == "" {
		return "", nil
	}

	tpl := t.findExtendedTemplate(tplPath)
	if tpl == nil {
		return "", fmt.Errorf("extender template %q is not defined", tplPath)
	}

	tpl = tpl.Lookup(tplName)
	if tpl == nil {
		return "", nil
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("execute %q -> %q: %w", tplPath, tplName, err)
	}

	return buf.String(), nil
}

func sensorCtx(endpoint int, device *config.Device, sensor sensor.Sensor, extender generator.Extender) SensorCtx {
	return SensorCtx{
		Endpoint: endpoint,
		Device:   device,
		Sensor:   sensor,
		Extender: extender,
	}
}

func clusterCtx(endpoint int, cluster cluster.Cluster) ClusterCtx {
	return ClusterCtx{
		Endpoint: endpoint,
		Cluster:  cluster,
	}
}

func isLast(i, arrLen int) bool {
	return i+1 == arrLen
}

func sum(a, b int) int {
	return a + b
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
