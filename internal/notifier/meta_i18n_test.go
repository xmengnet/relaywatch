package notifier

import (
	"strings"
	"testing"
	"unicode"
)

func containsHan(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Han, r) })
}

// TestMetaEnglishCoverage 保证每个内置渠道、每个字段都有英文文案。
func TestMetaEnglishCoverage(t *testing.T) {
	metas := NewDefaultRegistry().Metas()
	if len(metas) == 0 {
		t.Fatal("no notifier metas registered")
	}
	for _, m := range metas {
		if m.LabelEn == "" {
			t.Errorf("notifier %s: missing label_en", m.Type)
		}
		if containsHan(m.LabelEn) {
			t.Errorf("notifier %s: label_en contains Chinese: %q", m.Type, m.LabelEn)
		}
		for _, f := range m.Fields {
			if f.LabelEn == "" {
				t.Errorf("notifier %s field %s: missing label_en", m.Type, f.Key)
			}
			if containsHan(f.LabelEn) {
				t.Errorf("notifier %s field %s: label_en contains Chinese: %q", m.Type, f.Key, f.LabelEn)
			}
			if f.Help != "" && f.HelpEn == "" {
				t.Errorf("notifier %s field %s: missing help_en", m.Type, f.Key)
			}
			if containsHan(f.HelpEn) {
				t.Errorf("notifier %s field %s: help_en contains Chinese: %q", m.Type, f.Key, f.HelpEn)
			}
		}
	}
}

// TestMetaTranslationHasNoOrphans 防止翻译表里残留已删除的渠道或字段。
func TestMetaTranslationHasNoOrphans(t *testing.T) {
	metas := NewDefaultRegistry().Metas()
	byType := make(map[string]Meta, len(metas))
	for _, m := range metas {
		byType[m.Type] = m
	}
	for typ, tr := range metaEN {
		m, ok := byType[typ]
		if !ok {
			t.Errorf("metaEN has unknown notifier %q", typ)
			continue
		}
		keys := make(map[string]bool, len(m.Fields))
		for _, f := range m.Fields {
			keys[f.Key] = true
		}
		for key := range tr.fields {
			if !keys[key] {
				t.Errorf("metaEN[%s] has unknown field %q", typ, key)
			}
		}
	}
}
