package service

import (
	"fmt"
	"github.com/arttor/helmify/pkg/helmify"
	"github.com/arttor/helmify/pkg/processor"
	yamlformat "github.com/arttor/helmify/pkg/yaml"
	"github.com/iancoleman/strcase"
	"io"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"
	"text/template"
)

var ingressTempl, _ = template.New("ingress").Parse(
	`{{ .If }}
{{ .Meta }}
{{ .Spec }}
{{ .End }}`)

var ingressGVC = schema.GroupVersionKind{
	Group:   "networking.k8s.io",
	Version: "v1",
	Kind:    "Ingress",
}

// NewIngress creates processor for k8s Ingress resource.
func NewIngress() helmify.Processor {
	return &ingress{}
}

type ingress struct{}

// Process k8s Service object into template. Returns false if not capable of processing given resource type.
func (r ingress) Process(appMeta helmify.AppMetadata, obj *unstructured.Unstructured) (bool, helmify.Template, error) {
	if obj.GroupVersionKind() != ingressGVC {
		return false, nil, nil
	}
	ing := networkingv1.Ingress{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &ing)
	if err != nil {
		return true, nil, fmt.Errorf("%w: unable to cast to ingress", err)
	}

	av := helmify.Values{}
	meta, err := processor.ProcessObjMeta(appMeta, obj, processor.WithAnnotations(av))
	if err != nil {
		return true, nil, err
	}
	name := appMeta.TrimName(obj.GetName())
	shortName := strings.TrimPrefix(name, "controller-manager-")
	shortNameCamel := strcase.ToLowerCamel(shortName)

	processIngressSpec(appMeta, &ing.Spec)

	values := helmify.Values{}

	processIngressEnabled(shortNameCamel, ing, values)

	if err = processIngressClassName(shortNameCamel, &ing.Spec, values); err != nil {
		return false, nil, err
	}

	spec, err := yamlformat.Marshal(map[string]interface{}{"spec": &ing.Spec}, 0)
	if err != nil {
		return true, nil, err
	}

	a := obj.GetAnnotations()
	_ = unstructured.SetNestedStringMap(values, a, shortNameCamel, "ingress", "annotations")

	return true, &ingressResult{
		name: name + ".yaml",
		data: struct {
			If   string
			Meta string
			Spec string
			End  string
		}{
			If:   fmt.Sprintf("{{- if .Values.%s.ingress.enabled }}", shortNameCamel),
			Meta: meta,
			Spec: spec,
			End:  "{{- end }}",
		},
		values: values,
	}, nil
}

func processIngressClassName(shortNameCamel string, ingSpec *networkingv1.IngressSpec, values helmify.Values) error {
	var className string

	if ingSpec.IngressClassName != nil {
		className = *ingSpec.IngressClassName
	}
	_ = unstructured.SetNestedField(values, className, shortNameCamel, "ingress", "className")
	className = fmt.Sprintf("{{.Values.%s.ingress.className}}", shortNameCamel)

	ingSpec.IngressClassName = &className
	return nil
}

func processIngressEnabled(shortNameCamel string, ing networkingv1.Ingress, values helmify.Values) {
	_ = unstructured.SetNestedField(values, true, shortNameCamel, "ingress", "enabled")
}

func processIngressSpec(appMeta helmify.AppMetadata, ing *networkingv1.IngressSpec) {
	if ing.DefaultBackend != nil && ing.DefaultBackend.Service != nil {
		ing.DefaultBackend.Service.Name = appMeta.TemplatedName(ing.DefaultBackend.Service.Name)
	}
	for i := range ing.Rules {
		if ing.Rules[i].IngressRuleValue.HTTP != nil {
			for j := range ing.Rules[i].IngressRuleValue.HTTP.Paths {
				if ing.Rules[i].IngressRuleValue.HTTP.Paths[j].Backend.Service != nil {
					ing.Rules[i].IngressRuleValue.HTTP.Paths[j].Backend.Service.Name = appMeta.TemplatedName(ing.Rules[i].IngressRuleValue.HTTP.Paths[j].Backend.Service.Name)
				}
			}
		}
	}
}

type ingressResult struct {
	name string
	data struct {
		If   string
		Meta string
		Spec string
		End  string
	}
	values helmify.Values
}

func (r *ingressResult) Filename() string {
	return r.name
}

func (r *ingressResult) Values() helmify.Values {
	return r.values
}

func (r *ingressResult) Write(writer io.Writer) error {
	return ingressTempl.Execute(writer, r.data)
}
