package handler

import (
	"errors"
	"fmt"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nektro/andesite/pkg/db"
	"github.com/nektro/andesite/pkg/idata"
	"github.com/nektro/andesite/pkg/itypes"
	"github.com/nektro/andesite/pkg/iutil"
	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"github.com/nektro/go-util/arrays/stringsu"
	"github.com/nektro/go-util/util"
	etc "github.com/nektro/go.etc"
	oauth2 "github.com/nektro/go.oauth2"
	"github.com/valyala/fastjson"

	. "github.com/nektro/go-util/alias"
)

func HandleDirectoryListing(getAccess func(http.ResponseWriter, *http.Request) (string, string, []string, *itypes.User, map[string]interface{}, error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		fileRoot, qpath, uAccess, user, extras, err := getAccess(w, r)
		fileRoot, _ = filepath.Abs(fileRoot)

		w.Header().Add("access-control-allow-origin", "*")

		// if getAccess errored, response has already been written
		if err != nil {
			return
		}

		// disallow path tricks
		if strings.Contains(r.URL.Path, "..") {
			return
		}

		// disallow exploring dotfile folders
		if strings.Contains(qpath, "/.") {
			iutil.WriteUserDenied(r, w, true, false)
			return
		}

		// valid path check
		stat, err := os.Stat(fileRoot + qpath)
		if os.IsNotExist(err) {
			// 404
			iutil.WriteUserDenied(r, w, true, false)
			return
		}

		// server file/folder
		if stat.IsDir() {

			// ensure directory paths end in `/`
			if !strings.HasSuffix(r.URL.Path, "/") {
				w.Header().Set("Location", r.URL.Path+"/")
				w.WriteHeader(http.StatusFound)
				return
			}

			// get list of all files
			files, _ := ioutil.ReadDir(fileRoot + qpath)

			// hide dot files
			files = iutil.Filter(files, func(x os.FileInfo) bool {
				return !strings.HasPrefix(x.Name(), ".")
			})

			// amount of files in the directory
			l1 := len(files)

			// access check
			files = iutil.Filter(files, func(x os.FileInfo) bool {
				ok := false
				fpath := qpath + x.Name()
				for _, item := range uAccess {
					if strings.HasPrefix(item, fpath) || strings.HasPrefix(qpath, item) {
						ok = true
					}
				}
				return ok
			})

			// amount of files given access to
			l2 := len(files)

			if l1 > 0 && l2 == 0 {
				iutil.WriteUserDenied(r, w, true, false)
				return
			}

			data := make([]map[string]interface{}, len(files))
			gi := 0
			for i := 0; i < len(files); i++ {
				name := files[i].Name()
				a := ""
				if files[i].IsDir() || files[i].Mode()&os.ModeSymlink != 0 {
					a = name + "/"
				} else {
					a = name
				}
				ext := filepath.Ext(a)
				if files[i].IsDir() {
					ext = ".folder"
				}
				if len(ext) == 0 {
					ext = ".asc"
				}
				data[gi] = map[string]interface{}{
					"name":    a,
					"size":    util.ByteCountIEC(files[i].Size()),
					"mod":     files[i].ModTime().UTC().String()[:19],
					"ext":     ext[1:],
					"mod_raw": strconv.FormatInt(files[i].ModTime().UTC().Unix(), 10),
					"is_file": !files[i].IsDir(),
				}
				gi++
			}
			pth := r.URL.Path[len(idata.Config.HTTPBase)-1:]
			printer := message.NewPrinter(language.English)
			dsize, fcount := db.FolderSize(pth)

			etc.WriteHandlebarsFile(r, w, "/listing.hbs", map[string]interface{}{
				"version":    idata.Version,
				"provider":   user.Provider,
				"user":       user,
				"can_search": db.CanSearch,
				"path":       pth,
				"files":      data,
				"admin":      user.Admin,
				"base":       idata.Config.HTTPBase,
				"name":       oauth2.ProviderIDMap[user.Provider].NamePrefix + user.Name,
				"host":       util.FullHost(r),
				"extras":     extras,
				"file_count": printer.Sprintf("%d", len(files)),
				"dir_size":   util.ByteCountIEC(dsize),
				"file_total": printer.Sprintf("%d", fcount),
			})
		} else {
			// access check
			can := false
			for _, item := range uAccess {
				if strings.HasPrefix(qpath, item) {
					can = true
				}
			}
			if can == false {
				iutil.WriteUserDenied(r, w, true, false)
				return
			}

			w.Header().Add("Content-Type", mime.TypeByExtension(path.Ext(qpath)))
			file, _ := os.Open(fileRoot + qpath)
			info, _ := os.Stat(fileRoot + qpath)
			http.ServeContent(w, r, info.Name(), info.ModTime(), file)
		}
	}
}

// handler for http://andesite/files/*
func HandleFileListing(w http.ResponseWriter, r *http.Request) (string, string, []string, *itypes.User, map[string]interface{}, error) {
	_, user, err := iutil.ApiBootstrap(r, w, []string{http.MethodGet, http.MethodHead}, true, false, true)
	if err != nil {
		return "", "", nil, nil, nil, err
	}
	u := strings.Split(r.URL.Path, "/")

	userAccess := db.QueryAccess(user)
	dc := idata.Config.GetDiscordClient()

	if user.Provider == "discord" && dc.Extra1 != "" && dc.Extra2 != "" {
		dra := db.QueryAllDiscordRoleAccess()
		var p fastjson.Parser

		rurl := F("%s/guilds/%s/members/%s", idata.DiscordAPI, dc.Extra1, user.Snowflake)
		req, _ := http.NewRequest(http.MethodGet, rurl, strings.NewReader(""))
		req.Header.Set("User-Agent", "nektro/andesite")
		req.Header.Set("Authorization", "Bot "+dc.Extra2)
		bys := util.DoHttpRequest(req)
		v, err := p.Parse(string(bys))
		if err != nil {
			fmt.Println(2, "err", err.Error())
		}
		if v != nil {
			for _, item := range dra {
				for _, i := range v.GetArray("roles") {
					s, _ := i.StringBytes()
					if string(s) == item.RoleID {
						userAccess = append(userAccess, item.Path)
					}
				}
			}
		}
	}
	userAccess = stringsu.Filter(userAccess, func(s string) bool {
		return strings.HasPrefix(s, "/"+u[1]+"/") || s == "/"
	})
	userAccess = stringsu.Map(userAccess, func(s string) string {
		if s == "/" {
			return s
		}
		return s[len(u[1])+1:]
	})

	dp, qpath, err := processListingURL(idata.DataPathsPrv, r.URL.Path)
	if err != nil {
		iutil.WriteResponse(r, w, "Not Found", "", "")
		return "", "", nil, nil, nil, errors.New("")
	}
	return dp, qpath, userAccess, user, map[string]interface{}{
		"user": user,
	}, nil
}

// handler for http://andesite/public/*
func HandlePublicListing(w http.ResponseWriter, r *http.Request) (string, string, []string, *itypes.User, map[string]interface{}, error) {
	_, user, err := iutil.ApiBootstrap(r, w, []string{http.MethodHead, http.MethodGet}, false, false, true)
	if err != nil {
		return "", "", nil, nil, nil, err
	}
	dp, qpath, err := processListingURL(idata.DataPathsPub, r.URL.Path)
	if err != nil {
		iutil.WriteResponse(r, w, "Not Found", "", "")
		return "", "", nil, nil, nil, errors.New("")
	}
	return dp, qpath, []string{"/"}, user, map[string]interface{}{}, nil
}

// handler for http://andesite/open/*
func HandleShareListing(w http.ResponseWriter, r *http.Request) (string, string, []string, *itypes.User, map[string]interface{}, error) {
	_, user, err := iutil.ApiBootstrap(r, w, []string{http.MethodGet, http.MethodHead}, false, false, true)
	if err != nil {
		return "", "", nil, nil, nil, err
	}
	u := strings.Split(r.URL.Path, "/")
	if len(u) <= 3 {
		w.Header().Add("Location", "../")
		w.WriteHeader(http.StatusFound)
	}
	s := db.QueryAccessByShare(u[2])
	if len(s) == 0 {
		iutil.WriteResponse(r, w, "Not Found", "", "")
		return "", "", nil, nil, nil, errors.New("")
	}
	dp, ua, err := findRootForShareAccess(s)
	if err != nil {
		iutil.WriteResponse(r, w, "Not Found", "", "")
		return "", "", nil, nil, nil, errors.New("")
	}
	return dp, "/" + strings.Join(u[3:], "/"), []string{ua}, user, nil, nil
}
