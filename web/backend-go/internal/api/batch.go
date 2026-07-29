package api

import (
	"io"
	"net/http"
)

func (s *Server) batchUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Missing file")
		return
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, 16<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := header.Filename
	if name == "" {
		name = "upload.csv"
	}
	s.createBatch(w, r, string(content), name)
}

func (s *Server) batchUploadText(w http.ResponseWriter, r *http.Request) {
	var b struct {
		CSVText string `json:"csv_text"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}
	s.createBatch(w, r, b.CSVText, "paste.csv")
}

func (s *Server) createBatch(w http.ResponseWriter, r *http.Request, text, filename string) {
	opts := map[string]any{
		"dry_run":  queryBool(r, "dry_run", false),
		"ssl1":     queryBool(r, "ssl1", true),
		"ssl2":     queryBool(r, "ssl2", true),
		"automap":  queryBool(r, "automap", true),
		"usecache": queryBool(r, "usecache", true),
	}
	batch, err := s.Batch.Create(userFrom(r).ID, text, filename, opts)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, batch)
}

func (s *Server) batchStart(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Batch not found")
		return
	}
	user := userFrom(r)
	batch, err := s.DB.BatchByID(id, user.ID)
	if storeErr(w, err, "Batch not found") {
		return
	}
	if batch.BatchData == nil {
		writeError(w, http.StatusBadRequest, "Batch data not found")
		return
	}
	if batch.Status == "running" {
		writeError(w, http.StatusBadRequest, "Batch is already running")
		return
	}

	if err := s.DB.SetBatchStatus(batch.ID, "running"); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.Batch.Start(batch.ID, user.ID, batch.BatchData)

	batch, _ = s.DB.BatchByID(id, user.ID)
	writeJSON(w, http.StatusOK, batch)
}

func (s *Server) getBatch(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Batch not found")
		return
	}
	batch, err := s.DB.BatchByID(id, userFrom(r).ID)
	if storeErr(w, err, "Batch not found") {
		return
	}
	writeJSON(w, http.StatusOK, batch)
}

func (s *Server) browseFolders(w http.ResponseWriter, r *http.Request) {
	var b struct {
		// Either name a stored profile, or pass credentials directly. The
		// profile form exists because the browser is most useful when picking
		// folders for a profile, whose password the client does not have.
		ProfileID *int64 `json:"profile_id"`
		Host      string `json:"host"`
		User      string `json:"user"`
		Password  string `json:"password"`
		SSL       *bool  `json:"ssl"`
	}
	if !decodeJSON(w, r, &b) {
		return
	}

	host, user, password, ssl := b.Host, b.User, b.Password, boolOr(b.SSL, true)
	if b.ProfileID != nil && *b.ProfileID > 0 {
		p, err := s.DB.ProfileByID(*b.ProfileID, userFrom(r).ID)
		if storeErr(w, err, "Profile not found") {
			return
		}
		pw, err := s.Sec.Decrypt(p.Password1Enc)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		host, user, password, ssl = p.Host1, p.User1, pw, p.SSL1
	}
	if host == "" || user == "" || password == "" {
		writeError(w, http.StatusBadRequest, "Provide profile_id, or host, user and password")
		return
	}

	folders, err := s.Folders.Browse(host, user, password, ssl)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, folders)
}

func (s *Server) batchAbort(w http.ResponseWriter, r *http.Request) {
	id, ok := urlID(r)
	if !ok {
		writeError(w, http.StatusNotFound, "Batch not found")
		return
	}
	batch, err := s.DB.BatchByID(id, userFrom(r).ID)
	if storeErr(w, err, "Batch not found") {
		return
	}
	if !s.Batch.Cancel(batch.ID) {
		writeError(w, http.StatusBadRequest, "This batch is not running.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "aborting"})
}
