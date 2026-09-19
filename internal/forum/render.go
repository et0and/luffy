package forum

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// rowStyles matches forest.py: even rows use thread_row, odd use
// thread_row_alt.
var rowStyles = [2]string{"thread_row", "thread_row_alt"}

func (s *Store) htmlTop() string {
	return fmt.Sprintf(`<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd">
<html xmlns="http://www.w3.org/1999/xhtml">
 <head>
  <meta http-equiv="Content-Type" content="text/html; charset=utf-8" />
  <title>%s</title>
  <link rel="stylesheet" href="%s" type="text/css" />
  <script type="text/javascript">
  function show(elem_id) {
    elem = document.getElementById(elem_id);
    if (elem) elem.style.display = 'block';
  }
  </script>
 </head>
<body>
<h3>%s</h3>
<p class="board_description">%s</p>
`, s.cfg.BoardTitle, s.cfg.CSSPath, s.cfg.BoardTitle, s.cfg.BoardDescription)
}

const htmlBottom = `
<p class="smallprint" >Powered by the <a href="http://www.triv.org.uk/~nelis/forest">Forest Python Board</a></font>
</body></html>`

const htmlThreadsTop = `<table width="95%" class="threads_table">
 <tr class="threads_header">
  <th width="60%">Subject</th><th>Author</th><th>Date</th><th>Replies</th><th>Last Reply</th>
 </tr>
`

const htmlThreadsRow = `
 <tr class="%s">
  <td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td>
 </tr>
`

func (s *Store) htmlNewThread() string {
	return fmt.Sprintf(`
<p><a href="javascript:show('hidden_form');">Start a new thread</a></p>
<div id="hidden_form">
<form method="post" action="?new=thread">
<p>Name: <input name="author" maxlength="%d"/></p>
<p>Subject: <input name="subject" maxlength="%d" size="80"/></p>
<p><textarea name="body" rows="10" cols="80"></textarea></p>
<p><input class="submit_button" type="submit" value="New Thread!"/></p>
</form>
</div>
`, s.cfg.MaxAuthorLen, s.cfg.MaxSubjectLen)
}

func (s *Store) htmlNewReply(threadHash string) string {
	return fmt.Sprintf(`
<p><a href="javascript:show('hidden_form')">Reply to this thread</a></p>
<div id="hidden_form">
<form method="post" action="?new=reply&amp;thread=%s">
<p>Name: <input name="author" maxlength="%d"/></p>
<p><textarea name="body" rows="10" cols="80"></textarea></p>
<p><input class="submit_button" type="submit" value="Reply!"/></p>
</form>
</div>
`, threadHash, s.cfg.MaxAuthorLen)
}

const htmlThreadsBottom = `</table>`

const htmlThreadTop = `<table width="95%%" class="threads_table">
 <col width="15%%" />
 <col width="85%%" />
 <tr><td colspan="2"><a href="?">&lt;&lt; Main</a></td></tr>
 <tr class="thread_header"><td colspan="2">%s</td></tr>
`

const htmlThreadRow = `
 <tr class="%s">
  <td valign="top"><b>%s</b><br/><small>%s</small></td>
  <td>%s</td>
 </tr>
`

const htmlThreadBottom = `
</table>
`

// RenderPagingLinks reproduces forest.display_paging_links.
func (s *Store) RenderPagingLinks(currentOffset, numItems, pageLength int, threadHash string) string {
	if pageLength <= 0 {
		return ""
	}
	pages := numItems / pageLength
	if numItems%pageLength != 0 {
		pages++
	}
	if pages < 2 {
		return ""
	}
	links := make([]string, 0, pages)
	for pageNumber := 0; pageNumber < pages; pageNumber++ {
		offset := pageNumber * pageLength
		label := strconv.Itoa(pageNumber + 1)
		if offset == currentOffset {
			links = append(links, label)
			continue
		}
		var href string
		if threadHash != "" {
			href = "?" + url.Values{"thread": {threadHash}, "offset": {strconv.Itoa(offset)}}.Encode()
		} else {
			href = "?offset=" + strconv.Itoa(offset)
		}
		links = append(links, fmt.Sprintf(`<a href="%s">%s</a>`, href, label))
	}
	return strings.Join(links, " | ")
}

// RenderIndex renders the thread list page.
func (s *Store) RenderIndex(summaries []Summary, total, offset int) string {
	var b strings.Builder
	b.WriteString(s.htmlTop())
	if links := s.RenderPagingLinks(offset, total, s.cfg.IndexPageSize, ""); links != "" {
		b.WriteString(links)
		b.WriteString("\n")
	}
	b.WriteString(htmlThreadsTop)
	for i, thread := range summaries {
		link := fmt.Sprintf(`<a href="?thread=%s">%s</a>`, thread.Hash, thread.Subject)
		b.WriteString(fmt.Sprintf(htmlThreadsRow,
			rowStyles[i%2], link, thread.Author, thread.Date, thread.Replies, thread.LastReply))
	}
	b.WriteString(htmlThreadsBottom)
	b.WriteString(s.htmlNewThread())
	b.WriteString(htmlBottom)
	return b.String()
}

// RenderThread renders a single thread page.
func (s *Store) RenderThread(thread *Thread, offset int) string {
	return s.RenderThreadWithError(thread, offset, "")
}

// RenderThreadWithError renders a thread page with an optional error banner,
// matching how forest.py printed post errors after the header.
func (s *Store) RenderThreadWithError(thread *Thread, offset int, errMsg string) string {
	var b strings.Builder
	b.WriteString(s.htmlTop())
	if errMsg != "" {
		b.WriteString(errMsg)
		b.WriteString("\n")
	}
	if links := s.RenderPagingLinks(offset, thread.Total, s.cfg.ThreadPageSize, thread.Hash); links != "" {
		b.WriteString(links)
		b.WriteString("\n")
	}
	b.WriteString(fmt.Sprintf(htmlThreadTop, thread.Subject))
	for i, post := range thread.Posts {
		b.WriteString(fmt.Sprintf(htmlThreadRow, rowStyles[i%2], post.Author, post.Date, post.Body))
	}
	b.WriteString(htmlThreadBottom)
	b.WriteString(s.htmlNewReply(thread.Hash))
	b.WriteString(htmlBottom)
	return b.String()
}

// RenderErrorPage renders the index with an error message placed where
// forest.py printed post errors: directly after the header block.
func (s *Store) RenderErrorPage(errMsg string, summaries []Summary, total, offset int) string {
	page := s.RenderIndex(summaries, total, offset)
	marker := "</p>\n"
	idx := strings.Index(page, marker)
	if idx < 0 {
		return errMsg + page
	}
	cut := idx + len(marker)
	return page[:cut] + errMsg + "\n" + page[cut:]
}

// RenderStandaloneError renders just the frame with an error, matching
// forest.list_single_thread when the thread is invalid.
func (s *Store) RenderStandaloneError(errMsg string) string {
	return s.htmlTop() + errMsg + "\n" + htmlBottom
}

// RenderRedirect returns a page that sends the browser to the new thread,
// mirroring forest.redirect (the original relied on a meta refresh because
// CGI redirects were unreliable on old routers).
func (s *Store) RenderRedirect(threadHash string, offset int) string {
	location := s.cfg.CGIURL + "?thread=" + url.QueryEscape(threadHash)
	if offset > 0 {
		location += "&offset=" + strconv.Itoa(offset)
	}
	return fmt.Sprintf(`<html><head><meta http-equiv="refresh" content="0;url=%s"></head></html>`, location)
}
