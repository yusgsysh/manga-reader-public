package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "encoding/json/v2"
)

const mockTorrentsHTML = `<!DOCTYPE html><html><head></head><body>
<div class="stuffbox">
<div id="torrentinfo" style="height:535px; width:100%; overflow:auto">
<div style="height:450px; overflow:auto; margin:auto">
<h1 style="font-size:10pt; font-weight:bold; margin:3px; text-align:center">[Yanje] Test</h1>
<form method="post" action="https://exhentai.org/gallerytorrents.php?gid=4225833&amp;t=92249546c3">
<div style="margin:10px 5px; padding:3px; border:1px solid #C7B5A3">
<input type="hidden" name="gtid" value="2250081" />
<table style="width:99%">
<tr>
 <td style="width:180px"><span style="font-weight:bold">Posted:</span> <span>2026-10-02 06:30</span></td>
 <td style="width:150px"><span style="font-weight:bold">Size:</span> 42.13 MiB</td>
 <td></td>
 <td style="width:80px"><span style="font-weight:bold">Seeds:</span> 8</td>
 <td style="width:80px"><span style="font-weight:bold">Peers:</span> 4</td>
 <td style="width:100px; text-align:center"><span style="font-weight:bold">Downloads:</span> 12</td>
</tr>
<tr>
 <td colspan="5"><span style="font-weight:bold">Uploader:</span> Konazumi</td>
 <td rowspan="2" style="width:100px; text-align:center"><input type="submit" name="torrent_info" value="Information" style="width:80px" /></td>
</tr>
<tr>
 <td colspan="5"> &nbsp; <a href="https://exhentai.org/torrent/4225833/98e51ff1f6815ede2484eb899c107545068a685e.torrent" onclick="document.location='https://exhentai.org/torrent/4225833/seg/98e51ff1f6815ede2484eb899c107545068a685e.torrent'; return false">[Yanje] Test [中国翻訳].zip</a></td>
</tr>
</table>
</div>
</form>
</div>
<div style="margin:auto; border-top:1px solid #5C0D12">
<span style="font-weight:bold">New Torrents:</span>
<form method="post" action="https://upld.exhentai.org/upld/torrent_post.php?gid=4225833&amp;t=92249546c3" enctype="multipart/form-data">
<div style="height:30px; line-height:30px; vertical-align:middle">
<input type="hidden" name="MAX_FILE_SIZE" value="10485760" />
<input type="file" name="torrentfile" accept="application/x-bittorrent" />
<input type="submit" name="torrent_upload" value="Upload Torrent" />
</div>
</form>
</div>
</div>
</body></html>`

const mockTorrentsEmptyHTML = `<!DOCTYPE html><html><head></head><body>
<div id="torrentinfo"><div><h1>No torrents</h1>
<form method="post" action="https://exhentai.org/gallerytorrents.php?gid=1&amp;t=2"></form>
</div></div>
</body></html>`

const mockTorrentInfoHTML = `<!DOCTYPE html><html><head></head><body>
<div id="torrentinfo" style="height:535px; width:100%; overflow:auto">
<h1 style="font-size:9pt; font-weight:bold; margin:3px; text-align:center">[Yanje] Test.zip</h1>
<table id="ett">
 <tr>
  <td style="font-weight:bold; width:100px">Posted</td><td>2026-10-02 06:30</td>
  <td style="font-weight:bold; width:80px">Seeds</td><td>9</td>
 </tr>
 <tr>
  <td style="font-weight:bold; width:100px">Uploader</td><td>Konazumi</td>
  <td style="font-weight:bold; width:80px">DLers</td><td>5</td>
 </tr>
 <tr>
  <td style="font-weight:bold; width:100px">Size</td><td>42.13 MiB</td>
  <td style="font-weight:bold; width:80px">Completes</td><td>12</td>
 </tr>
</table>
<table style="width:80%; margin:8px auto">
 <tr><td><a href="https://exhentai.org/torrent/4225833/1234567-seg/98e51ff1f6815ede2484eb899c107545068a685e.torrent" style="font-weight:bold; text-decoration:none">Personalized Torrent</a></td><td>(Just For You)</td></tr>
 <tr><td><a href="https://exhentai.org/torrent/4225833/98e51ff1f6815ede2484eb899c107545068a685e.torrent" onclick="document.location='x'; return false" style="font-weight:bold; text-decoration:none">Redistributable Torrent</a></td><td>(use if you want to share)</td></tr>
</table>
<div id="etd">No comments were given for this torrent.</div>
</div>
</body></html>`

func newTorrentsMockServer(listHTML, infoHTML, torrentBody string) *httptest.Server {
	return newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".torrent") {
			w.Header().Set("Content-Type", "application/x-bittorrent")
			_, _ = w.Write([]byte(torrentBody))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if r.Method == http.MethodPost {
			fmt.Fprint(w, infoHTML)
			return
		}
		fmt.Fprint(w, listHTML)
	})
}

func TestMockGalleryTorrents_List(t *testing.T) {
	srv := newTorrentsMockServer(mockTorrentsHTML, mockTorrentInfoHTML, "torrent-bytes")
	defer srv.Close()

	server := &Server{Client: newMockClient(srv.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/4225833/tok/torrents", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}

	var resp struct {
		Torrents []struct {
			GTID      string `json:"gtid"`
			Name      string `json:"name"`
			Size      string `json:"size"`
			Posted    string `json:"posted"`
			Seeds     int    `json:"seeds"`
			Peers     int    `json:"peers"`
			Downloads int    `json:"downloads"`
			Uploader  string `json:"uploader"`
		} `json:"torrents"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v. body: %s", err, w.Body.String())
	}
	if len(resp.Torrents) != 1 {
		t.Fatalf("torrents = %d, want 1", len(resp.Torrents))
	}
	got := resp.Torrents[0]
	if got.GTID != "2250081" || got.Size != "42.13 MiB" || got.Posted != "2026-10-02 06:30" ||
		got.Seeds != 8 || got.Peers != 4 || got.Downloads != 12 || got.Uploader != "Konazumi" {
		t.Errorf("torrent = %+v", got)
	}
	if !strings.Contains(got.Name, "Test") {
		t.Errorf("name = %q", got.Name)
	}
	if strings.Contains(w.Body.String(), "ehtracker") || strings.Contains(w.Body.String(), "/torrent/") {
		t.Error("response must not leak upstream download links")
	}
}

func TestMockGalleryTorrents_Empty(t *testing.T) {
	srv := newTorrentsMockServer(mockTorrentsEmptyHTML, "", "torrent-bytes")
	defer srv.Close()

	server := &Server{Client: newMockClient(srv.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/1/tok/torrents", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !strings.Contains(w.Body.String(), `"torrents":[]`) {
		t.Errorf("body = %s, want empty torrents", w.Body.String())
	}
}

func TestMockGalleryTorrents_Info(t *testing.T) {
	srv := newTorrentsMockServer(mockTorrentsHTML, mockTorrentInfoHTML, "torrent-bytes")
	defer srv.Close()

	server := &Server{Client: newMockClient(srv.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/4225833/tok/torrents/2250081/info", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var info struct {
		Posted       string `json:"posted"`
		Seeds        int    `json:"seeds"`
		Uploader     string `json:"uploader"`
		Dlers        int    `json:"dlers"`
		Size         string `json:"size"`
		Completes    int    `json:"completes"`
		Comments     string `json:"comments"`
		Personalized bool   `json:"personalized"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if info.Seeds != 9 || info.Dlers != 5 || info.Completes != 12 ||
		info.Posted != "2026-10-02 06:30" || info.Size != "42.13 MiB" || info.Uploader != "Konazumi" {
		t.Errorf("info = %+v", info)
	}
	if !strings.Contains(info.Comments, "No comments") {
		t.Errorf("comments = %q", info.Comments)
	}
	if !info.Personalized {
		t.Error("personalized should be true")
	}
}

func TestMockGalleryTorrents_Download(t *testing.T) {
	srv := newTorrentsMockServer(mockTorrentsHTML, mockTorrentInfoHTML, "torrent-bytes")
	defer srv.Close()

	server := &Server{Client: newMockClient(srv.URL)}
	r := setupMockRouter(server)

	for _, variant := range []string{"", "?variant=redistributable", "?variant=personalized"} {
		req := httptest.NewRequest("GET", "/api/gallery/4225833/tok/torrents/2250081/download"+variant, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("variant %q: status = %d, want %d. body: %s", variant, w.Code, http.StatusOK, w.Body.String())
		}
		if w.Body.String() != "torrent-bytes" {
			t.Errorf("variant %q: body = %q", variant, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/x-bittorrent" {
			t.Errorf("variant %q: Content-Type = %q", variant, ct)
		}
		if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".torrent") {
			t.Errorf("variant %q: Content-Disposition = %q", variant, cd)
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("variant %q: Cache-Control = %q", variant, cc)
		}
	}
}

func TestMockGalleryTorrents_DownloadNotFound(t *testing.T) {
	srv := newTorrentsMockServer(mockTorrentsHTML, mockTorrentInfoHTML, "torrent-bytes")
	defer srv.Close()

	server := &Server{Client: newMockClient(srv.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/4225833/tok/torrents/999/download", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
