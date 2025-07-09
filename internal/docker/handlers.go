package docker

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// handleBase handles GET /v2/
func (r *Registry) handleBase(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}

// handleCatalog handles GET /v2/_catalog
func (r *Registry) handleCatalog(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	repos := make([]string, 0, len(r.manifests))
	for repo := range r.manifests {
		repos = append(repos, repo)
	}

	response := map[string]interface{}{
		"repositories": repos,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleTagsList handles GET /v2/{name}/tags/list
func (r *Registry) handleTagsList(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	name := vars["name"]

	r.mu.RLock()
	defer r.mu.RUnlock()

	tags := []string{}
	if repoManifests, exists := r.manifests[name]; exists {
		for ref := range repoManifests {
			// Only include tags, not digests
			if !strings.HasPrefix(ref, "sha256:") {
				tags = append(tags, ref)
			}
		}
	}

	response := map[string]interface{}{
		"name": name,
		"tags": tags,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleManifestGet handles GET/HEAD /v2/{name}/manifests/{reference}
func (r *Registry) handleManifestGet(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	name := vars["name"]
	reference := vars["reference"]

	r.mu.RLock()
	defer r.mu.RUnlock()

	repoManifests, exists := r.manifests[name]
	if !exists {
		r.writeError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", "manifest not found", nil)
		return
	}

	manifest, exists := repoManifests[reference]
	if !exists {
		r.writeError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", "manifest not found", nil)
		return
	}

	// Calculate digest
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifest.Raw))

	// Set headers
	w.Header().Set("Content-Type", manifest.MediaType)
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Length", strconv.Itoa(len(manifest.Raw)))

	if req.Method == "HEAD" {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(manifest.Raw)
}

// handleManifestPut handles PUT /v2/{name}/manifests/{reference}
func (r *Registry) handleManifestPut(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	name := vars["name"]
	reference := vars["reference"]

	// Read manifest body
	body, err := io.ReadAll(req.Body)
	if err != nil {
		r.writeError(w, http.StatusBadRequest, "MANIFEST_INVALID", "failed to read manifest", nil)
		return
	}

	// Parse manifest to validate
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		r.writeError(w, http.StatusBadRequest, "MANIFEST_INVALID", "invalid manifest json", nil)
		return
	}

	// Store raw manifest data
	manifest.Raw = body

	// Get content type from header or detect from manifest
	contentType := req.Header.Get("Content-Type")
	if contentType == "" {
		contentType = manifest.MediaType
	}
	manifest.MediaType = contentType

	// Calculate digest
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))

	r.mu.Lock()
	if _, exists := r.manifests[name]; !exists {
		r.manifests[name] = make(map[string]*Manifest)
	}
	
	// Store by reference (tag or digest)
	r.manifests[name][reference] = &manifest
	
	// Also store by digest if reference is a tag
	if !strings.HasPrefix(reference, "sha256:") {
		r.manifests[name][digest] = &manifest
	}
	r.mu.Unlock()

	// Store manifest in storage backend
	manifestPath := path.Join("manifests", digest)
	if err := r.storage.Store(name, manifestPath, bytes.NewReader(body)); err != nil {
		r.writeError(w, http.StatusInternalServerError, "MANIFEST_BLOB_UNKNOWN", "failed to store manifest", nil)
		return
	}

	// Set headers
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/manifests/%s", name, digest))
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

// handleManifestDelete handles DELETE /v2/{name}/manifests/{reference}
func (r *Registry) handleManifestDelete(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	name := vars["name"]
	reference := vars["reference"]

	r.mu.Lock()
	defer r.mu.Unlock()

	repoManifests, exists := r.manifests[name]
	if !exists {
		r.writeError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", "manifest not found", nil)
		return
	}

	if _, exists := repoManifests[reference]; !exists {
		r.writeError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", "manifest not found", nil)
		return
	}

	delete(repoManifests, reference)

	// Delete from storage
	manifestPath := path.Join("manifests", reference)
	_ = r.storage.Delete(name, manifestPath)

	w.WriteHeader(http.StatusAccepted)
}

// handleBlobGet handles GET/HEAD /v2/{name}/blobs/{digest}
func (r *Registry) handleBlobGet(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	name := vars["name"]
	digest := vars["digest"]

	blobPath := path.Join("blobs", digest)
	
	// Check if blob exists
	exists, err := r.storage.Exists(name, blobPath)
	if err != nil || !exists {
		r.writeError(w, http.StatusNotFound, "BLOB_UNKNOWN", "blob not found", nil)
		return
	}

	if req.Method == "HEAD" {
		// For HEAD request, just return headers
		// In a real implementation, we'd store blob metadata
		w.Header().Set("Docker-Content-Digest", digest)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Retrieve blob
	reader, err := r.storage.Retrieve(name, blobPath)
	if err != nil {
		r.writeError(w, http.StatusNotFound, "BLOB_UNKNOWN", "blob not found", nil)
		return
	}
	defer reader.Close()

	// Set headers
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Type", "application/octet-stream")

	// Copy blob to response
	w.WriteHeader(http.StatusOK)
	io.Copy(w, reader)
}

// handleBlobDelete handles DELETE /v2/{name}/blobs/{digest}
func (r *Registry) handleBlobDelete(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	name := vars["name"]
	digest := vars["digest"]

	blobPath := path.Join("blobs", digest)
	
	if err := r.storage.Delete(name, blobPath); err != nil {
		r.writeError(w, http.StatusNotFound, "BLOB_UNKNOWN", "blob not found", nil)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// handleBlobUploadPost handles POST /v2/{name}/blobs/uploads/
func (r *Registry) handleBlobUploadPost(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	name := vars["name"]

	// Create new upload session
	uploadUUID := uuid.New().String()
	upload := &Upload{
		UUID:      uploadUUID,
		RepoName:  name,
		StartedAt: time.Now(),
		Data:      []byte{},
	}

	r.mu.Lock()
	r.uploads[uploadUUID] = upload
	r.mu.Unlock()

	// Set headers
	location := fmt.Sprintf("/v2/%s/blobs/uploads/%s", name, uploadUUID)
	w.Header().Set("Location", location)
	w.Header().Set("Docker-Upload-UUID", uploadUUID)
	w.Header().Set("Range", "bytes=0-0")
	w.WriteHeader(http.StatusAccepted)
}

// handleBlobUploadPatch handles PATCH /v2/{name}/blobs/uploads/{uuid}
func (r *Registry) handleBlobUploadPatch(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	name := vars["name"]
	uploadUUID := vars["uuid"]

	r.mu.Lock()
	upload, exists := r.uploads[uploadUUID]
	if !exists {
		r.mu.Unlock()
		r.writeError(w, http.StatusNotFound, "BLOB_UPLOAD_UNKNOWN", "upload not found", nil)
		return
	}
	r.mu.Unlock()

	// Read chunk data
	chunk, err := io.ReadAll(req.Body)
	if err != nil {
		r.writeError(w, http.StatusBadRequest, "BLOB_UPLOAD_INVALID", "failed to read chunk", nil)
		return
	}

	// Append to upload data
	r.mu.Lock()
	upload.Data = append(upload.Data, chunk...)
	upload.Size = int64(len(upload.Data))
	r.mu.Unlock()

	// Set headers
	location := fmt.Sprintf("/v2/%s/blobs/uploads/%s", name, uploadUUID)
	w.Header().Set("Location", location)
	w.Header().Set("Docker-Upload-UUID", uploadUUID)
	w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", upload.Size-1))
	w.WriteHeader(http.StatusAccepted)
}

// handleBlobUploadPut handles PUT /v2/{name}/blobs/uploads/{uuid}
func (r *Registry) handleBlobUploadPut(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	name := vars["name"]
	uploadUUID := vars["uuid"]

	// Get expected digest from query parameter
	digest := req.URL.Query().Get("digest")
	if digest == "" {
		r.writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "digest parameter required", nil)
		return
	}

	r.mu.Lock()
	upload, exists := r.uploads[uploadUUID]
	if !exists {
		r.mu.Unlock()
		r.writeError(w, http.StatusNotFound, "BLOB_UPLOAD_UNKNOWN", "upload not found", nil)
		return
	}

	// Read any remaining data
	if req.ContentLength > 0 {
		chunk, err := io.ReadAll(req.Body)
		if err != nil {
			r.mu.Unlock()
			r.writeError(w, http.StatusBadRequest, "BLOB_UPLOAD_INVALID", "failed to read chunk", nil)
			return
		}
		upload.Data = append(upload.Data, chunk...)
	}

	// Calculate actual digest
	actualDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(upload.Data))
	if actualDigest != digest {
		r.mu.Unlock()
		r.writeError(w, http.StatusBadRequest, "DIGEST_INVALID", "digest mismatch", nil)
		return
	}

	// Remove from uploads
	delete(r.uploads, uploadUUID)
	r.mu.Unlock()

	// Store blob
	blobPath := path.Join("blobs", digest)
	if err := r.storage.Store(name, blobPath, bytes.NewReader(upload.Data)); err != nil {
		r.writeError(w, http.StatusInternalServerError, "BLOB_UPLOAD_INVALID", "failed to store blob", nil)
		return
	}

	// Set headers
	location := fmt.Sprintf("/v2/%s/blobs/%s", name, digest)
	w.Header().Set("Location", location)
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

// handleBlobUploadGet handles GET /v2/{name}/blobs/uploads/{uuid}
func (r *Registry) handleBlobUploadGet(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	uploadUUID := vars["uuid"]

	r.mu.RLock()
	upload, exists := r.uploads[uploadUUID]
	r.mu.RUnlock()

	if !exists {
		r.writeError(w, http.StatusNotFound, "BLOB_UPLOAD_UNKNOWN", "upload not found", nil)
		return
	}

	w.Header().Set("Docker-Upload-UUID", uploadUUID)
	w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", upload.Size-1))
	w.WriteHeader(http.StatusNoContent)
}

// handleBlobUploadDelete handles DELETE /v2/{name}/blobs/uploads/{uuid}
func (r *Registry) handleBlobUploadDelete(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	uploadUUID := vars["uuid"]

	r.mu.Lock()
	delete(r.uploads, uploadUUID)
	r.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}