package upload

import (
	"fmt"
	"html"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// styleBlock is the droopy stylesheet, kept close to the original so the
// iframe on the start page keeps looking the same.
const styleBlock = `
<meta name="viewport" content="width=device-width,initial-scale=1,maximum-scale=1" />
<style type="text/css">
* {margin: 0; padding: 0;}
body {text-align: center; font-family: sans-serif; color:#777;}
div {word-wrap: break-word;}
img {max-width: 100%;}
a {color: #4499cc; text-decoration: none;}
.container {max-width: 700px; margin: auto; background-color: #fff;}
#message {padding: 5px 0; font-size: 2em; font-weight: lighter;
          letter-spacing: -2px; line-height: 50px; color: #aaa;}
#sending {display: none; font-style: italic;}
#wrapform { height: 30px;  padding-top:30px;}
#userinfo {padding-bottom: 20px;}
#files {margin: auto; padding: 13px 0; text-align: left; overflow: auto; margin-bottom: 20px;}
#files a {text-decoration: none; display: block; padding: 10px 20px;}
#files a:nth-child(2n+1) {background-color: #F7F7F7;}
</style>`

const scriptBlock = `
<script language="JavaScript">
function show(elem_id) {
    elem = document.getElementById(elem_id);
    if (elem) elem.style.display = 'block';
}
function swap() {
   document.getElementById("form").style.display = "none";
   document.getElementById("sending").style.display = "block";
}
</script>`

// translations holds the droopy strings for the languages PirateBox
// installations used most. Unknown languages fall back to English.
var translations = map[string]map[string]string{
	"en": {
		"maintitle": "Send a file", "submit": "Send", "sending": "Sending",
		"successtitle": "File received", "received": "File received!",
		"another": "Send another file.", "errortitle": "Problem",
		"problem": "There has been a problem!", "retry": "Retry.",
		"subdirerror":     "There is a problem uploading to that subdirectory.",
		"advanced":        "advanced options",
		"userdirname":     "Name of directory to place files in",
		"userdirpassword": "Password for saving files to that directory (optional)",
	},
	"de": {
		"maintitle": "Datei senden", "submit": "Senden", "sending": "Sendet",
		"successtitle": "Datei empfangen", "received": "Datei empfangen!",
		"another": "Weitere Datei senden", "errortitle": "Fehler",
		"problem": "Ein Fehler ist aufgetreten!", "retry": "Wiederholen",
		"subdirerror":     "Beim Hochladen in dieses Unterverzeichnis ist ein Problem aufgetreten.",
		"advanced":        "erweiterte Optionen",
		"userdirname":     "Name des Zielverzeichnisses",
		"userdirpassword": "Passwort zum Speichern in diesem Verzeichnis (optional)",
	},
	"es": {
		"maintitle": "Enviar un archivo", "submit": "Enviar", "sending": "Enviando",
		"successtitle": "Archivo recibido", "received": "¡Archivo recibido!",
		"another": "Enviar otro archivo.", "errortitle": "Error",
		"problem": "¡Hubo un problema!", "retry": "Reintentar",
		"subdirerror":     "Hubo un problema al subir a ese subdirectorio.",
		"advanced":        "opciones avanzadas",
		"userdirname":     "Nombre del directorio de destino",
		"userdirpassword": "Contraseña para guardar en ese directorio (opcional)",
	},
	"fr": {
		"maintitle": "Envoyer un fichier", "submit": "Envoyer", "sending": "Envoi en cours",
		"successtitle": "Fichier reçu", "received": "Fichier reçu !",
		"another": "Envoyer un autre fichier.", "errortitle": "Problème",
		"problem": "Il y a eu un problème !", "retry": "Réessayer.",
		"subdirerror":     "Un problème est survenu lors de l'envoi vers ce sous-répertoire.",
		"advanced":        "options avancées",
		"userdirname":     "Nom du répertoire de destination",
		"userdirpassword": "Mot de passe pour ce répertoire (facultatif)",
	},
	"nl": {
		"maintitle": "Verstuur een bestand", "submit": "Verstuur", "sending": "Bezig met versturen",
		"successtitle": "Bestand ontvangen", "received": "Bestand ontvangen!",
		"another": "Verstuur nog een bestand.", "errortitle": "Fout",
		"problem": "Er is een fout opgetreden!", "retry": "Nog eens.",
		"subdirerror":     "Er ging iets mis bij het uploaden naar die map.",
		"advanced":        "geavanceerde opties",
		"userdirname":     "Naam van de doelmap",
		"userdirpassword": "Wachtwoord voor die map (optioneel)",
	},
	"pt-br": {
		"maintitle": "Enviar um arquivo", "submit": "Enviar", "sending": "Enviando",
		"successtitle": "Arquivo recebido", "received": "Arquivo recebido!",
		"another": "Enviar outro arquivo.", "errortitle": "Erro",
		"problem": "Ocorreu um erro!", "retry": "Tentar novamente.",
		"subdirerror":     "Ocorreu um erro ao enviar para esse subdiretório.",
		"advanced":        "opções avançadas",
		"userdirname":     "Nome do diretório de destino",
		"userdirpassword": "Senha para salvar nesse diretório (opcional)",
	},
	"ru": {
		"maintitle": "Отправить файл", "submit": "Отправить", "sending": "Отправляю",
		"successtitle": "Файл получен", "received": "Файл получен !",
		"another": "Отправить другой файл.", "errortitle": "Ошибка",
		"problem": "Произошла ошибка !", "retry": "Повторить.",
		"subdirerror":     "Не удалось загрузить файл в этот подкаталог.",
		"advanced":        "дополнительные параметры",
		"userdirname":     "Имя каталога назначения",
		"userdirpassword": "Пароль для этого каталога (необязательно)",
	},
	"zh-cn": {
		"maintitle": "发送文件", "submit": "发送", "sending": "发送中",
		"successtitle": "文件已收到", "received": "文件已收到！",
		"another": "发送另一个文件。", "errortitle": "问题",
		"problem": "出现问题！", "retry": "重试。",
		"subdirerror":     "上传到该子目录时出现问题。",
		"advanced":        "高级选项",
		"userdirname":     "目标目录名称",
		"userdirpassword": "该目录的保存密码（可选）",
	},
}

// preferredLanguage implements droopy's Accept-Language parsing: entries
// are ordered by q value, then the first known translation wins.
func preferredLanguage(header string) string {
	if header == "" {
		return "en"
	}
	type candidate struct {
		lang string
		q    float64
		pos  int
	}
	var candidates []candidate
	for pos, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lang := part
		q := 1.0
		if semi := strings.Index(part, ";q="); semi >= 0 {
			lang = strings.TrimSpace(part[:semi])
			if parsed, err := strconv.ParseFloat(strings.TrimSpace(part[semi+3:]), 64); err == nil {
				q = parsed
			}
		}
		candidates = append(candidates, candidate{lang: strings.ToLower(lang), q: q, pos: pos})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].q == candidates[j].q {
			return candidates[i].pos < candidates[j].pos
		}
		return candidates[i].q > candidates[j].q
	})
	for _, c := range candidates {
		if _, ok := translations[c.lang]; ok {
			return c.lang
		}
		if base := strings.SplitN(c.lang, "-", 2)[0]; base != "" {
			if _, ok := translations[base]; ok {
				return base
			}
		}
	}
	return "en"
}

func (h *Handler) strings(r *http.Request) map[string]string {
	lang := preferredLanguage(r.Header.Get("Accept-Language"))
	table, ok := translations[lang]
	if !ok {
		table = translations["en"]
	}
	out := make(map[string]string, len(translations["en"])+1)
	for k, v := range translations["en"] {
		out[k] = v
	}
	for k, v := range table {
		out[k] = v
	}
	if h.Message != "" {
		out["message"] = `<div id="message">` + html.EscapeString(h.Message) + `</div>`
	}
	return out
}

type pageKind int

const (
	pageMain pageKind = iota
	pageSuccess
	pageError
)

func (h *Handler) servePage(w http.ResponseWriter, r *http.Request, status int, kind pageKind, errorKey string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprint(w, h.render(h.strings(r), kind, errorKey))
}

func (h *Handler) render(t map[string]string, kind pageKind, errorKey string) string {
	title := t["maintitle"]
	body := ""
	switch kind {
	case pageSuccess:
		title = t["successtitle"]
		body = `<div class="box">` + t["received"] + ` <a href="/">` + t["another"] + `</a></div>`
	case pageError:
		title = t["errortitle"]
		message := t["problem"]
		if errorKey != "" && t[errorKey] != "" {
			message = t[errorKey]
		}
		body = `<div class="box">` + message + ` <a href="/">` + t["retry"] + `</a></div>`
	default:
		subdirFields := ""
		if h.UserDirs {
			subdirFields = `
      <br><a href="javascript:show('advanced_fields')">` + t["advanced"] + `</a>
      <fieldset id="advanced_fields" style="display:none">
        <br>` + t["userdirname"] + ` <input name="subdir" type="textbox">
        <p>` + t["userdirpassword"] + ` <input name="secret" type="textbox">
      </fieldset>`
		}
		body = `<div id="form" class="box">
    <form method="post" enctype="multipart/form-data" action="">
      <input name="upfile" type="file" multiple="yes">
      <input value="` + t["submit"] + `" onclick="swap()" type="submit">` + subdirFields + `
    </form>
  </div>
  <div id="sending" class="box"><div class="text">` + t["sending"] + `</div></div>`
	}
	return `<!doctype html>
<html>
<head><title>` + title + `</title>` + styleBlock + scriptBlock + `</head>
<body>
<div class="container">
<div id="wrapform">` + body + `</div>
<div id="userinfo">` + t["message"] + `</div>
</div>
</body>
</html>`
}
