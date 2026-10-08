package fileprocessor

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Processor handles file processing operations
type Processor struct{}

// NewProcessor creates a new file processor
func NewProcessor() *Processor {
	return &Processor{}
}

// GetPageCount returns the number of pages in a PDF or DJVU file
func (p *Processor) GetPageCount(filePath string) (int, error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".pdf":
		return p.getPDFPageCount(filePath)
	case ".djvu":
		return p.getDJVUPageCount(filePath)
	default:
		return 0, fmt.Errorf("unsupported file type: %s", ext)
	}
}

// ExtractPageImage extracts a page as an image (PNG)
func (p *Processor) ExtractPageImage(filePath string, pageNum int, outputDir string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".pdf":
		return p.extractPDFPageImage(filePath, pageNum, outputDir)
	case ".djvu":
		return p.extractDJVUPageImage(filePath, pageNum, outputDir)
	default:
		return "", fmt.Errorf("unsupported file type: %s", ext)
	}
}

// ExtractPageText extracts text content from a page
func (p *Processor) ExtractPageText(filePath string, pageNum int) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".pdf":
		return p.extractPDFPageText(filePath, pageNum)
	case ".djvu":
		return p.extractDJVUPageText(filePath, pageNum)
	default:
		return "", fmt.Errorf("unsupported file type: %s", ext)
	}
}

// getPDFPageCount gets the page count from a PDF file using pdfinfo
func (p *Processor) getPDFPageCount(filePath string) (int, error) {
	// Try using pdfinfo (from poppler-utils)
	cmd := exec.Command("pdfinfo", filePath)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to run pdfinfo: %w (make sure poppler-utils is installed)", err)
	}

	// Parse output to find "Pages:" line
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Pages:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				count, err := strconv.Atoi(parts[1])
				if err == nil {
					return count, nil
				}
			}
		}
	}

	return 0, fmt.Errorf("could not find page count in pdfinfo output")
}

// getDJVUPageCount gets the page count from a DJVU file using djvused
func (p *Processor) getDJVUPageCount(filePath string) (int, error) {
	// Use djvused to get page count
	cmd := exec.Command("djvused", "-e", "n", filePath)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to run djvused: %w (make sure djvulibre-bin is installed)", err)
	}

	count, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return 0, fmt.Errorf("failed to parse page count: %w", err)
	}

	return count, nil
}

// extractPDFPageImage extracts a page from PDF as PNG using pdftoppm
func (p *Processor) extractPDFPageImage(filePath string, pageNum int, outputDir string) (string, error) {
	// Output filename
	outputPath := filepath.Join(outputDir, fmt.Sprintf("page_%d.png", pageNum))

	// Use pdftoppm to convert PDF page to PNG
	// -f: first page, -l: last page, -png: output as PNG, -r: resolution (150 DPI)
	cmd := exec.Command(
		"pdftoppm",
		"-f", strconv.Itoa(pageNum),
		"-l", strconv.Itoa(pageNum),
		"-png",
		"-r", "150",
		"-singlefile",
		filePath,
		strings.TrimSuffix(outputPath, ".png"),
	)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to extract PDF page image: %w (make sure poppler-utils is installed)", err)
	}

	return outputPath, nil
}

// extractDJVUPageImage extracts a page from DJVU as PNG using ddjvu
func (p *Processor) extractDJVUPageImage(filePath string, pageNum int, outputDir string) (string, error) {
	outputPath := filepath.Join(outputDir, fmt.Sprintf("page_%d.png", pageNum))

	// Use ddjvu to convert DJVU page to PNG
	cmd := exec.Command(
		"ddjvu",
		"-format=png",
		"-page="+strconv.Itoa(pageNum),
		"-scale=150",
		filePath,
		outputPath,
	)

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to extract DJVU page image: %w (make sure djvulibre-bin is installed)", err)
	}

	return outputPath, nil
}

// extractPDFPageText extracts text from a PDF page using pdftotext
func (p *Processor) extractPDFPageText(filePath string, pageNum int) (string, error) {
	// Use pdftotext to extract text from a specific page
	cmd := exec.Command(
		"pdftotext",
		"-f", strconv.Itoa(pageNum),
		"-l", strconv.Itoa(pageNum),
		filePath,
		"-", // Output to stdout
	)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to extract PDF text: %w (make sure poppler-utils is installed)", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// extractDJVUPageText extracts text from a DJVU page using djvutxt
func (p *Processor) extractDJVUPageText(filePath string, pageNum int) (string, error) {
	// Use djvutxt to extract text from a specific page
	cmd := exec.Command(
		"djvutxt",
		"--page="+strconv.Itoa(pageNum),
		filePath,
	)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to extract DJVU text: %w (make sure djvulibre-bin is installed)", err)
	}

	return strings.TrimSpace(string(output)), nil
}
