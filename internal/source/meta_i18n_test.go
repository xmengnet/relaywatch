package source

import (
	"strings"
	"testing"
	"unicode"
)

func containsHan(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return unicode.Is(unicode.Han, r) })
}

// TestMetaEnglishCoverage 保证每个内置源、每个字段都有英文文案，
// 新增源/字段时若忘记翻译会在这里失败。
func TestMetaEnglishCoverage(t *testing.T) {
	metas := NewDefaultRegistry().Metas()
	if len(metas) == 0 {
		t.Fatal("no source metas registered")
	}
	for _, m := range metas {
		if m.LabelEn == "" {
			t.Errorf("source %s: missing label_en", m.Type)
		}
		if containsHan(m.LabelEn) {
			t.Errorf("source %s: label_en contains Chinese: %q", m.Type, m.LabelEn)
		}
		for _, f := range m.Fields {
			if f.LabelEn == "" {
				t.Errorf("source %s field %s: missing label_en", m.Type, f.Key)
			}
			if containsHan(f.LabelEn) {
				t.Errorf("source %s field %s: label_en contains Chinese: %q", m.Type, f.Key, f.LabelEn)
			}
			if f.Help != "" && f.HelpEn == "" {
				t.Errorf("source %s field %s: missing help_en", m.Type, f.Key)
			}
			if containsHan(f.HelpEn) {
				t.Errorf("source %s field %s: help_en contains Chinese: %q", m.Type, f.Key, f.HelpEn)
			}
			if containsHan(f.Placeholder) && f.PlaceholderEn == "" {
				t.Errorf("source %s field %s: missing placeholder_en", m.Type, f.Key)
			}
		}
	}
}

// TestMetaTranslationHasNoOrphans 防止翻译表里残留已删除的源或字段。
func TestMetaTranslationHasNoOrphans(t *testing.T) {
	metas := NewDefaultRegistry().Metas()
	byType := make(map[string]Meta, len(metas))
	for _, m := range metas {
		byType[m.Type] = m
	}
	for typ, tr := range metaEN {
		m, ok := byType[typ]
		if !ok {
			t.Errorf("metaEN has unknown source %q", typ)
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
