package images

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/containers/buildah/define"
	"github.com/containers/storage/pkg/archive"
	"github.com/stretchr/testify/assert"
)

func TestBuildMatchIID(t *testing.T) {
	assert.True(t, iidRegex.MatchString("a883dafc480d466ee04e0d6da986bd78eb1fdd2178d04693723da3a8f95d42f4"))
	assert.True(t, iidRegex.MatchString("3da3a8f95d42"))
	assert.False(t, iidRegex.MatchString("3da3"))
}

func TestBuildNotMatchStatusMessage(t *testing.T) {
	assert.False(t, iidRegex.MatchString("Copying config a883dafc480d466ee04e0d6da986bd78eb1fdd2178d04693723da3a8f95d42f4"))
}

func TestConvertAdditionalBuildContexts(t *testing.T) {
	additionalBuildContexts := map[string]*define.AdditionalBuildContext{
		"context1": {
			IsURL:           false,
			IsImage:         false,
			Value:           "C:\\test",
			DownloadedCache: "",
		},
		"context2": {
			IsURL:           false,
			IsImage:         false,
			Value:           "/test",
			DownloadedCache: "",
		},
		"context3": {
			IsURL:           true,
			IsImage:         false,
			Value:           "https://a.com/b.tar",
			DownloadedCache: "",
		},
		"context4": {
			IsURL:           false,
			IsImage:         true,
			Value:           "quay.io/a/b:c",
			DownloadedCache: "",
		},
	}

	convertAdditionalBuildContexts(additionalBuildContexts)

	expectedGuestValues := map[string]string{
		"context1": "/mnt/c/test",
		"context2": "/test",
		"context3": "https://a.com/b.tar",
		"context4": "quay.io/a/b:c",
	}

	for key, value := range additionalBuildContexts {
		assert.Equal(t, expectedGuestValues[key], value.Value)
	}
}

func TestCreateTar(t *testing.T) {
	testCases := []struct {
		description   string
		setupFiles    func(t *testing.T, tempDir string) ([]string, []string)
		expectedFiles []string
		shouldError   bool
	}{
		{
			description: "Single file",
			setupFiles: func(t *testing.T, tempDir string) ([]string, []string) {
				filePath := filepath.Join(tempDir, "testfile.txt")
				err := os.WriteFile(filePath, []byte("hello world"), 0644)
				assert.NoError(t, err)
				return nil, []string{tempDir}
			},
			expectedFiles: []string{"testfile.txt"},
			shouldError:   false,
		},
		{
			description: "Multiple files",
			setupFiles: func(t *testing.T, tempDir string) ([]string, []string) {
				filePath1 := filepath.Join(tempDir, "file1.txt")
				filePath2 := filepath.Join(tempDir, "file2.txt")
				err := os.WriteFile(filePath1, []byte("content1"), 0644)
				assert.NoError(t, err)
				err = os.WriteFile(filePath2, []byte("content2"), 0644)
				assert.NoError(t, err)
				return nil, []string{tempDir}
			},
			expectedFiles: []string{"file1.txt", "file2.txt"},
			shouldError:   false,
		},
		{
			description: "Exclude default should produce empty tar",
			setupFiles: func(t *testing.T, tempDir string) ([]string, []string) {
				filePath1 := filepath.Join(tempDir, "file1.txt")
				err := os.WriteFile(filePath1, []byte("content1"), 0644)
				assert.NoError(t, err)
				return []string{tempDir}, []string{tempDir}
			},
			expectedFiles: []string{},
			shouldError:   false,
		},
		{
			description: "File after index 0 overwrite exclude",
			setupFiles: func(t *testing.T, tempDir string) ([]string, []string) {
				filePath1 := filepath.Join(tempDir, "file1.txt")
				err := os.WriteFile(filePath1, []byte("content1"), 0644)
				assert.NoError(t, err)
				return []string{tempDir}, []string{tempDir, "file1.txt"}
			},
			expectedFiles: []string{"file1.txt"},
			shouldError:   false,
		},
		{
			description: "Empty directory",
			setupFiles: func(t *testing.T, tempDir string) ([]string, []string) {
				return nil, []string{tempDir}
			},
			expectedFiles: []string{},
			shouldError:   false,
		},
		{
			description: "Exclude files",
			setupFiles: func(t *testing.T, tempDir string) ([]string, []string) {
				filePath1 := filepath.Join(tempDir, "include.txt")
				filePath2 := filepath.Join(tempDir, "exclude.txt")
				err := os.WriteFile(filePath1, []byte("include content"), 0644)
				assert.NoError(t, err)
				err = os.WriteFile(filePath2, []byte("exclude content"), 0644)
				assert.NoError(t, err)
				return []string{"exclude.txt"}, []string{tempDir}
			},
			expectedFiles: []string{"include.txt"},
			shouldError:   false,
		},
		{
			description: "Symbolic link",
			setupFiles: func(t *testing.T, tempDir string) ([]string, []string) {
				target := filepath.Join(tempDir, "target.txt")
				link := filepath.Join(tempDir, "link.txt")
				err := os.WriteFile(target, []byte("target content"), 0644)
				assert.NoError(t, err)
				err = os.Symlink(target, link)
				assert.NoError(t, err)
				return nil, []string{tempDir}
			},
			expectedFiles: []string{"link.txt", "target.txt"},
			shouldError:   false,
		},
		{
			description: "No source provided",
			setupFiles: func(t *testing.T, tempDir string) ([]string, []string) {
				return nil, []string{}
			},
			expectedFiles: nil,
			shouldError:   true,
		},
		{
			description: "Containerfile outside of context",
			setupFiles: func(t *testing.T, tempDir string) ([]string, []string) {
				// create source 1 folder
				source1 := filepath.Join(tempDir, "source1")
				err := os.Mkdir(source1, 0o655)
				assert.NoError(t, err)

				// populate source 1 folder
				filePath := filepath.Join(source1, "source1.txt")
				err = os.WriteFile(filePath, []byte("hello world"), 0644)
				assert.NoError(t, err)

				// create source 2 folder
				source2 := filepath.Join(tempDir, "source2")
				err = os.Mkdir(source2, 0o655)
				assert.NoError(t, err)

				// populate source 2 folder
				containerfile := filepath.Join(source2, "Containerfile")
				err = os.WriteFile(containerfile, []byte("hello world"), 0644)
				assert.NoError(t, err)

				return nil, []string{source1, containerfile}
			},
			expectedFiles: []string{
				"source1/source1.txt",
				"source2/Containerfile",
			},
			shouldError: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.description, func(t *testing.T) {
			// Setup temporary directory
			tempDir := t.TempDir()

			// Setup files and get source paths
			excludes, sources := testCase.setupFiles(t, tempDir)

			// Call CreateTar
			cpy := sources
			tarStream, err := nTar(excludes, cpy...)
			if testCase.shouldError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// Untar and check contents
			extractedDir := t.TempDir()
			err = untar(tarStream, extractedDir)
			assert.NoError(t, err)

			// Verify the expected files are present
			for _, expectedFile := range testCase.expectedFiles {
				_, err := os.Stat(filepath.Join(extractedDir, expectedFile))
				assert.NoError(t, err)
			}
		})
	}
}

// Helper function to untar the resulting tarball
func untar(tarStream io.Reader, destDir string) error {
	return archive.Untar(tarStream, destDir, nil)
}
