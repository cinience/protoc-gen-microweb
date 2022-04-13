package main

import (
	"strings"
	"text/template"

	"github.com/golang/protobuf/proto"
	pgs "github.com/lyft/protoc-gen-star"
	pgsgo "github.com/lyft/protoc-gen-star/lang/go"
	"google.golang.org/genproto/googleapis/api/annotations"
)

type handler struct {
	method  string
	pattern string
	body    string
}

type MicroWebModule struct {
	*pgs.ModuleBase
	ctx   pgsgo.Context
	tpl   *template.Template
	eimp  map[pgs.FilePath]string       // map[importPath] = alias
	pconv map[pgs.FilePath]pgs.FilePath // convert paths, mostly for google imports
}

func MicroWeb() *MicroWebModule {
	return &MicroWebModule{
		ModuleBase: &pgs.ModuleBase{},
	}
}

func (p *MicroWebModule) InitContext(ctx pgs.BuildContext) {
	p.ModuleBase.InitContext(ctx)
	p.ctx = pgsgo.InitContext(ctx.Parameters())

	tpl := template.New("microweb").Funcs(map[string]interface{}{
		"package":             p.ctx.PackageName,
		"name":                p.ctx.Name,
		"importPath":          p.ctx.ImportPath,
		"marshaler":           p.marshaler,
		"unmarshaler":         p.unmarshaler,
		"handlerPrefix":       p.handlerPrefix,
		"handlerMethod":       p.handlerMethod,
		"handlerPattern":      p.handlerPattern,
		"handlerBody":         p.handlerBody,
		"extraImports":        p.extraImports,
		"getExtraImportAlias": p.getExtraImportAlias,
	})

	p.eimp = make(map[pgs.FilePath]string)
	p.pconv = map[pgs.FilePath]pgs.FilePath{
		"google.golang.org/protobuf/types/known/emptypb": "github.com/golang/protobuf/ptypes/empty",
	}
	p.tpl = template.Must(tpl.Parse(microwebTpl))
}

// Name satisfies the generator.Plugin interface.
func (p *MicroWebModule) Name() string {
	return "web"
}

func (p *MicroWebModule) Execute(targets map[string]pgs.File, pkgs map[string]pgs.Package) []pgs.Artifact {
	ignored_packages := map[string]bool{}

	params := p.ctx.Params()
	if value, ok := params["ignore_packages"]; ok {
		for _, ignored_pkg := range strings.Split(value, ";") {
			ignored_packages[ignored_pkg] = true
		}
	}

	for _, t := range targets {
		packageName := t.Package().ProtoName().String()
		fileName := t.Name().String()
		if !ignored_packages[packageName] {
			p.generate(t)
		} else {
			p.Debugf("Ignoring filename %s because it belongs to the ignored package %s", fileName, packageName)
		}
	}

	return p.Artifacts()
}

func getHandler(m pgs.Method) *handler {
	opts := m.Descriptor().GetOptions()
	pext, _ := proto.GetExtension(opts, annotations.E_Http)
	ext, ok := pext.(*annotations.HttpRule)

	if !ok {
		return nil
	}

	switch {
	case ext.GetGet() != "":
		return &handler{method: "GET", pattern: ext.GetGet(), body: ext.GetBody()}
	case ext.GetPost() != "":
		return &handler{method: "POST", pattern: ext.GetPost(), body: ext.GetBody()}
	case ext.GetPut() != "":
		return &handler{method: "PUT", pattern: ext.GetPut(), body: ext.GetBody()}
	case ext.GetPatch() != "":
		return &handler{method: "PATCH", pattern: ext.GetPatch(), body: ext.GetBody()}
	case ext.GetDelete() != "":
		return &handler{method: "DELETE", pattern: ext.GetDelete(), body: ext.GetBody()}
	}

	return nil
}

func (p *MicroWebModule) generate(f pgs.File) {
	if len(f.Messages()) == 0 {
		return
	}

	name := p.ctx.OutputPath(f).SetExt(".web.go")
	p.AddGeneratorTemplateFile(name.String(), p.tpl, f)
}

func (p *MicroWebModule) handlerMethod(m pgs.Method) pgs.Name {
	return pgs.Name(getHandler(m).method)
}

func (p *MicroWebModule) handlerPattern(m pgs.Method) pgs.Name {
	return pgs.Name(getHandler(m).pattern)
}

func (p *MicroWebModule) handlerBody(m pgs.Method) pgs.Name {
	return pgs.Name(getHandler(m).body)
}

func (p *MicroWebModule) handlerPrefix(m pgs.Method) pgs.Name {
	return pgs.Name("")
}

func (p *MicroWebModule) marshaler(m pgs.Message) pgs.Name {
	return p.ctx.Name(m) + "JSONMarshaler"
}

func (p *MicroWebModule) unmarshaler(m pgs.Message) pgs.Name {
	return p.ctx.Name(m) + "JSONUnmarshaler"
}

func (p *MicroWebModule) extraImports(args ...pgs.FilePath) map[pgs.FilePath]string {
	var alias string

	for _, arg := range args {
		if value, exists := p.pconv[arg]; exists {
			arg = value
		}

		if _, ok := p.eimp[arg]; !ok {
			parts := strings.Split(arg.String(), "/")
			length := len(parts)
			if length < 2 {
				alias = arg.String()
			} else {
				alias = strings.Join(parts[length-2:], "")
			}
			p.eimp[arg] = alias
		}
	}
	return p.eimp
}

func (p *MicroWebModule) getExtraImportAlias(arg pgs.FilePath) string {
	if value, exists := p.pconv[arg]; exists {
		arg = value
	}

	return p.eimp[arg]
}

const microwebTpl = `// Code generated by protoc-gen-microweb. DO NOT EDIT.
// source: {{ name .}}.proto

package {{ package . }}

{{ $imports := extraImports }}
{{ range $idx, $svc := .Services }}
	{{ range .Methods }}
		{{ if ne (importPath .Input) (importPath .File) }}
			{{ $imports = extraImports (importPath .Input) }}
		{{ end }}
		{{ if ne (importPath .Output) (importPath .File) }}
			{{ $imports = extraImports (importPath .Output) }}
		{{ end }}
	{{ end }}
{{ end }}

import (
	"bytes"
	"encoding/json"
	{{ if gt (len .Services) 0 -}}
    "strings"
	"net/http"
	{{- end }}

	"github.com/golang/protobuf/jsonpb"
	{{ if gt (len .Services) 0 -}}
	"go-micro.dev/v4/errors"
    "github.com/mitchellh/mapstructure"
	"github.com/cinience/render"
	"github.com/go-chi/chi/v5"
	{{- end }}
	{{ range $key, $value := $imports }}
		{{ $value }} "{{ $key }}"
	{{- end }}
)

{{ range $idx, $svc := .Services }}
type web{{ $svc.Name }}Handler struct {
	r chi.Router
	h {{ $svc.Name }}Handler
}

func (h *web{{ $svc.Name }}Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.r.ServeHTTP(w, r)
}

{{ range .Methods }}
func (h *web{{ $svc.Name }}Handler) {{ name . }}(w http.ResponseWriter, r *http.Request) {
	{{ if ne (importPath .Input) (importPath .File) -}}
		req := &{{ getExtraImportAlias (importPath .Input) }}.{{ .Input.Name }}{}
	{{- else -}}
		req := &{{ .Input.Name }}{}
	{{- end }}
	{{ if ne (importPath .Output) (importPath .File) -}}
		resp := &{{ getExtraImportAlias (importPath .Output) }}.{{ .Output.Name }}{}
	{{- else -}}
		resp := &{{ .Output.Name }}{}
	{{- end }}

	{{ if ne (getExtraImportAlias (importPath .Input)) "ptypesempty" -}}
	contentType := r.Header.Get("Content-Type")
	if strings.Contains(contentType, "application/x-www-form-urlencoded")  {
		if err := r.ParseForm(); err != nil {
			render.Status(r, http.StatusOK)
			render.JSON(w, r, &struct {
				Reason  string
				Code    int32
				Detail  string
				Status  string
				Success bool
			}{"ParseRequestFailed", http.StatusBadRequest, err.Error(), "", false})

			return
		}
		m := make(map[string]interface{})
		for k, _ := range r.Form {
			v := r.Form.Get(k)
			if strings.HasPrefix(v, "{") && json.Valid([]byte(v)) {
				var vv map[string]interface{}
				if err := json.Unmarshal([]byte(v), &vv); err != nil {
					render.Status(r, http.StatusOK)
					render.JSON(w, r, &struct {
						Reason  string
						Code    int32
						Detail  string
						Status  string
						Success bool
					}{"ParseRequestFailed", http.StatusBadRequest, err.Error(), "", false})
					return
				}
				m[k] = vv
			} else {
				m[k] = v
			}
		}
		if err := mapstructure.WeakDecode(m, &req); err != nil {
		    render.Status(r, http.StatusOK)
			render.JSON(w, r, &struct {
				Reason  string
				Code    int32
				Detail  string
				Status  string
				Success bool
			}{"ParseRequestFailed", http.StatusBadRequest, err.Error(), "", false})
			
			return
		}
	} else if strings.Contains(contentType, "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			render.Status(r, http.StatusOK)
			render.JSON(w, r, &struct {
				Reason  string
				Code    int32
				Detail  string
				Status  string
				Success bool
			}{"ParseRequestFailed", http.StatusBadRequest, err.Error(), "", false})
			
			return
		}
	}
	{{- end }}

	if err := h.h.{{ name . }}(
		r.Context(),
		req,
		resp,
	); err != nil {
		status := errors.FromError(err)
		rst := &struct {
			Reason string
			Code   int32
			Detail string
			Status string
			Success bool
		}{status.Id, status.Code, status.Detail, status.Status, false}
		// render.Status(r, int(status.Code))
        render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, rst)
		return
	}

	{{ if ne (getExtraImportAlias (importPath .Output)) "ptypesempty" -}}
	{{- if eq (handlerMethod .) "POST" }}
	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
	{{- end }}
	{{- if eq (handlerMethod .) "DELETE" }}
	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
	{{- end }}
	{{- if eq (handlerMethod .) "PATCH" }}
	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
	{{- end }}
	{{- if eq (handlerMethod .) "PUT" }}
	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
	{{- end }}
	{{- if eq (handlerMethod .) "GET" }}
	render.Status(r, http.StatusOK)
	render.JSON(w, r, resp)
	{{- end }}
	{{- else }}
	render.Status(r, http.StatusNoContent)
	render.NoContent(w, r)
	{{- end }}
}
{{ end }}

func Register{{ .Name }}Web(r chi.Router, i {{ .Name }}Handler, middlewares ...func(http.Handler) http.Handler) {
	handler := &web{{ .Name }}Handler{
		r: r,
		h: i,
	}

	{{ range .Methods }}{{ if .ServerStreaming }}{{- else -}}
	r.MethodFunc("{{ handlerMethod . }}", "{{ handlerPrefix . }}{{ handlerPattern . }}", handler.{{ name .}})
	{{ end }}{{- end -}}
}

{{ end }}

{{ range .AllMessages }}

// {{ marshaler . }} describes the default jsonpb.Marshaler used by all
// instances of {{ name . }}. This struct is safe to replace or modify but
// should not be done so concurrently.
var {{ marshaler . }} = new(jsonpb.Marshaler)

// MarshalJSON satisfies the encoding/json Marshaler interface. This method
// uses the more correct jsonpb package to correctly marshal the message.
func (m *{{ name . }}) MarshalJSON() ([]byte, error) {
	if m == nil {
		return json.Marshal(nil)
	}

	buf := &bytes.Buffer{}

	if err := {{ marshaler . }}.Marshal(buf, m); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

var _ json.Marshaler = (*{{ name . }})(nil)

// {{ unmarshaler . }} describes the default jsonpb.Unmarshaler used by all
// instances of {{ name . }}. This struct is safe to replace or modify but
// should not be done so concurrently.
var {{ unmarshaler . }} = new(jsonpb.Unmarshaler)

// UnmarshalJSON satisfies the encoding/json Unmarshaler interface. This method
// uses the more correct jsonpb package to correctly unmarshal the message.
func (m *{{ name . }}) UnmarshalJSON(b []byte) error {
	return {{ unmarshaler . }}.Unmarshal(bytes.NewReader(b), m)
}

var _ json.Unmarshaler = (*{{ name . }})(nil)

{{ end }}
`
