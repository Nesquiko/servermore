package commander_test

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	commanderapi "github.com/Nesquiko/servermore/pkg/api/commander"
	commonapi "github.com/Nesquiko/servermore/pkg/api/common"
	"github.com/Nesquiko/servermore/pkg/server"
	testutils "github.com/Nesquiko/servermore/test/test_utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateFunction_Created(t *testing.T) {
	t.Parallel()

	funcName := "testing-guest"
	binaryBytes := randomBinary(t, 2048)
	bodyFile, contentType := createFunctionMultipartBodyFromBytes(
		t,
		funcName,
		"testing-guest.bin",
		binaryBytes,
	)

	req, err := http.NewRequest(http.MethodPost, HttpServerUrl+"/functions", bodyFile)
	require.NoError(t, err, "create request")
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "send request")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var created commanderapi.Function
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created), "decode response")

	assert.NotZero(t, created.Id)
	assert.Equal(t, funcName, created.Name)

	queries := testutils.TestDB(t, DbFilePath)
	dbFunc, err := queries.FunctionById(t.Context(), created.Id)
	require.NoError(t, err, "query created function by id")

	assert.Equal(t, created.Id, dbFunc.ID)
	assert.Equal(t, created.Name, dbFunc.Name)
	assert.FileExists(t, dbFunc.Path)
	assert.DirExists(t, TestStorageRoot)
	assert.Equal(t, TestStorageRoot, filepath.Dir(dbFunc.Path))

	savedBytes, err := os.ReadFile(dbFunc.Path)
	require.NoError(t, err, "read saved function file")
	assert.Equal(t, binaryBytes, savedBytes)
}

func TestCreateFunction_ConflictOnDuplicate(t *testing.T) {
	t.Parallel()
	binaryBytes := randomBinary(t, 2048)

	bodyFile, contentType := createFunctionMultipartBodyFromBytes(
		t,
		"testing-guest-duplicate",
		"testing-guest-duplicate.bin",
		binaryBytes,
	)

	req, err := http.NewRequest(http.MethodPost, HttpServerUrl+"/functions", bodyFile)
	require.NoError(t, err, "create first request")
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "send first request")
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode, "first upload should succeed")

	bodyFile2, contentType2 := createFunctionMultipartBodyFromBytes(
		t,
		"testing-guest-duplicate",
		"testing-guest-duplicate.bin",
		binaryBytes,
	)

	req2, err := http.NewRequest(http.MethodPost, HttpServerUrl+"/functions", bodyFile2)
	require.NoError(t, err, "create second request")
	req2.Header.Set("Content-Type", contentType2)

	resp2, err := http.DefaultClient.Do(req2)
	require.NoError(t, err, "send second request")
	defer resp2.Body.Close()

	assert.Equal(t, http.StatusConflict, resp2.StatusCode)

	var apiErr commonapi.ErrorDetail
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&apiErr), "decode conflict response")

	assert.Equal(t, "function.exists", apiErr.Code)
	assert.Equal(t, http.StatusConflict, apiErr.Status)
	assert.Equal(t, "Function already exists", apiErr.Title)
	assert.Equal(t, "Function with same bytes already exists", apiErr.Detail)
}

func TestCreateFunction_BadRequestWhenTooLarge(t *testing.T) {
	t.Parallel()

	binaryBytes := randomBinary(t, server.MaxBytes+1)
	bodyFile, contentType := createFunctionMultipartBodyFromBytes(
		t,
		"too-large-function",
		"too-large-function.bin",
		binaryBytes,
	)

	req, err := http.NewRequest(http.MethodPost, HttpServerUrl+"/functions", bodyFile)
	require.NoError(t, err, "create request")
	req.Header.Set("Content-Type", contentType)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "send request")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var apiErr commonapi.ErrorDetail
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr), "decode too large response")

	assert.Equal(t, "function.too.large", apiErr.Code)
	assert.Equal(t, http.StatusBadRequest, apiErr.Status)
	assert.Equal(t, "Function binary too large", apiErr.Title)
}

func TestCreateFunction_BadRequestWhenMultipartMalformed(t *testing.T) {
	t.Parallel()

	body := bytes.NewBufferString(
		"--bad-boundary\r\nthis is not valid multipart\r\n--bad-boundary--\r\n",
	)
	req, err := http.NewRequest(http.MethodPost, HttpServerUrl+"/functions", body)
	require.NoError(t, err, "create malformed request")
	req.Header.Set("Content-Type", "multipart/form-data; boundary=bad-boundary")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "send malformed request")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var apiErr commonapi.ErrorDetail
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr), "decode malformed response")

	assert.Equal(t, http.StatusBadRequest, apiErr.Status)
	assert.NotEmpty(t, apiErr.Code)
	assert.NotEmpty(t, apiErr.Title)
	assert.NotEmpty(t, apiErr.Detail)
}

func TestCreateFunction_BadRequestWhenBinaryMissing(t *testing.T) {
	t.Parallel()

	bodyFile, err := os.CreateTemp(t.TempDir(), "create-function-missing-binary-*.multipart")
	require.NoError(t, err, "create temp multipart body file")

	writer := multipart.NewWriter(bodyFile)
	require.NoError(t, writer.WriteField("name", "missing-binary"), "write multipart name field")
	require.NoError(t, writer.Close(), "close multipart writer")

	_, err = bodyFile.Seek(0, 0)
	require.NoError(t, err, "rewind multipart body file")

	req, err := http.NewRequest(http.MethodPost, HttpServerUrl+"/functions", bodyFile)
	require.NoError(t, err, "create request")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "send request")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	var apiErr commonapi.ErrorDetail
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&apiErr), "decode missing binary response")

	assert.Equal(t, http.StatusBadRequest, apiErr.Status)
	assert.NotEmpty(t, apiErr.Code)
	assert.NotEmpty(t, apiErr.Title)
	assert.NotEmpty(t, apiErr.Detail)
}

func randomBinary(t *testing.T, size int) []byte {
	t.Helper()

	b := make([]byte, size)
	_, err := rand.Read(b)
	require.NoError(t, err, "generate random binary")

	return b
}

func createFunctionMultipartBodyFromBytes(
	t *testing.T,
	name string,
	filename string,
	binaryBytes []byte,
) (*os.File, string) {
	t.Helper()

	bodyFile, err := os.CreateTemp(t.TempDir(), "create-function-body-*.multipart")
	require.NoError(t, err, "create temp multipart body file")

	writer := multipart.NewWriter(bodyFile)
	require.NoError(t, writer.WriteField("name", name), "write multipart name field")

	part, err := writer.CreateFormFile("binary", filename)
	require.NoError(t, err, "create multipart file part")

	_, err = part.Write(binaryBytes)
	require.NoError(t, err, "write multipart binary part")
	require.NoError(t, writer.Close(), "close multipart writer")

	_, err = bodyFile.Seek(0, 0)
	require.NoError(t, err, "rewind multipart body file")

	return bodyFile, writer.FormDataContentType()
}
