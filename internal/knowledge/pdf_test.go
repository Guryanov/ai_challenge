package knowledge

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// buildTextPDF собирает минимальный PDF с текстовым слоем и корректным xref.
func buildTextPDF(t *testing.T, text string) []byte {
	t.Helper()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")

	offsets := make([]int, 6)
	writeObj := func(n int, body string) {
		offsets[n] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", n, body)
	}

	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] "+
		"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>")

	stream := "BT /F1 24 Tf 20 150 Td (" + escapePDFString(text) + ") Tj ET"
	writeObj(4, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	writeObj(5, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	xrefPos := buf.Len()
	buf.WriteString("xref\n0 6\n")
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefPos)

	return buf.Bytes()
}

func escapePDFString(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)")
	return r.Replace(s)
}

func TestUploadPDFExtractsText(t *testing.T) {
	svc, _ := newTestService(t, &fakeEmbedder{})
	data := buildTextPDF(t, "Hello PDF Knowledge Base")

	f, err := svc.UploadBytes(context.Background(), "p", "docs/guide.pdf", data)
	if err != nil {
		t.Fatalf("UploadBytes(pdf): %v", err)
	}
	if !strings.Contains(f.Content, "Hello PDF Knowledge Base") {
		t.Fatalf("extracted content = %q, want it to contain the PDF text", f.Content)
	}
	if f.Name != "guide.pdf" {
		t.Fatalf("name = %q, want guide.pdf", f.Name)
	}
	if f.SizeBytes != len(data) {
		t.Fatalf("size = %d, want %d", f.SizeBytes, len(data))
	}
}

func TestUploadPDFThenIndexAndSearch(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, &fakeEmbedder{})
	data := buildTextPDF(t, "alpha pdf document body")

	if _, err := svc.UploadBytes(ctx, "p", "alpha.pdf", data); err != nil {
		t.Fatalf("UploadBytes: %v", err)
	}
	if _, err := svc.Index(ctx, "p", "fixed"); err != nil {
		t.Fatalf("Index: %v", err)
	}
	results, err := svc.Search(ctx, "p", "fixed", "alpha", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 || results[0].Path != "alpha.pdf" {
		t.Fatalf("unexpected top result: %+v", results)
	}
}

func TestUploadPDFInvalidReturnsError(t *testing.T) {
	svc, _ := newTestService(t, &fakeEmbedder{})
	if _, err := svc.UploadBytes(context.Background(), "p", "broken.pdf", []byte("%PDF-1.4 not a real pdf")); err == nil {
		t.Fatal("expected error for invalid PDF")
	}
}

func TestUploadBinaryNonPDFReturnsError(t *testing.T) {
	svc, _ := newTestService(t, &fakeEmbedder{})
	if _, err := svc.UploadBytes(context.Background(), "p", "image.bin", []byte{0x00, 0x01, 0xff, 0xfe}); err == nil {
		t.Fatal("expected error for binary non-PDF file")
	}
}

func TestNormalizePDFText(t *testing.T) {
	got := normalizePDFText("line1\r\n\r\n\r\n\r\nline2   \n\n")
	want := "line1\n\nline2"
	if got != want {
		t.Fatalf("normalizePDFText = %q, want %q", got, want)
	}
}
