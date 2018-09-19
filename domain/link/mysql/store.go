// Copyright 2016 Documize Inc. <legal@documize.com>. All rights reserved.
//
// This software (Documize Community Edition) is licensed under
// GNU AGPL v3 http://www.gnu.org/licenses/agpl-3.0.en.html
//
// You can operate outside the AGPL restrictions by purchasing
// Documize Enterprise Edition and obtaining a commercial license
// by contacting <sales@documize.com>.
//
// https://documize.com

package mysql

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/documize/community/core/env"
	"github.com/documize/community/core/uniqueid"
	"github.com/documize/community/domain"
	"github.com/documize/community/domain/store/mysql"
	"github.com/documize/community/model/link"
	"github.com/pkg/errors"
)

// Scope provides data access to MySQL.
type Scope struct {
	Runtime *env.Runtime
}

// Add inserts wiki-link into the store.
// These links exist when content references another document or content.
func (s Scope) Add(ctx domain.RequestContext, l link.Link) (err error) {
	l.Created = time.Now().UTC()
	l.Revised = time.Now().UTC()

	_, err = ctx.Transaction.Exec("INSERT INTO dmz_doc_link (c_refid, c_orgid, c_spaceid, c_userid, c_sourcedocid, c_sourcesectionid, c_targetdocid, c_targetid, c_externalid, c_type, c_orphan, c_created, c_revised) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		l.RefID, l.OrgID, l.SpaceID, l.UserID, l.SourceDocumentID, l.SourceSectionID, l.TargetDocumentID, l.TargetID, l.ExternalID, l.LinkType, l.Orphan, l.Created, l.Revised)

	if err != nil {
		err = errors.Wrap(err, "execute link insert")
	}

	return
}

// GetDocumentOutboundLinks returns outbound links for specified document.
func (s Scope) GetDocumentOutboundLinks(ctx domain.RequestContext, documentID string) (links []link.Link, err error) {
	err = s.Runtime.Db.Select(&links, `
		select c_refid AS refid, c_orgid AS orgid, c_spaceid AS spaceid, c_userid AS userid,
        c_sourcedocid AS sourcedocumentid, c_sourcesectionid AS sourcesectionid,
        c_targetdocid AS targetdocumentid, c_targetid AS targetid, c_externalid AS externalid,
        c_type as linktype, c_orphan As orphan, c_created AS created, c_revised AS revised
		FROM dmz_doc_link
		WHERE c_orgid=? AND c_sourcedocid=?`,
		ctx.OrgID, documentID)

	if err != nil && err != sql.ErrNoRows {
		err = errors.Wrap(err, "select document oubound links")
		return
	}
	if len(links) == 0 {
		links = []link.Link{}
	}

	return
}

// GetPageLinks returns outbound links for specified page in document.
func (s Scope) GetPageLinks(ctx domain.RequestContext, documentID, pageID string) (links []link.Link, err error) {
	err = s.Runtime.Db.Select(&links, `
        select c_refid AS refid, c_orgid AS orgid, c_spaceid AS spaceid, c_userid AS userid,
        c_sourcedocid AS sourcedocumentid, c_sourcesectionid AS sourcesectionid,
        c_targetdocid AS targetdocumentid, c_targetid AS targetid, c_externalid AS externalid,
        c_type as linktype, c_orphan As orphan, c_created AS created, c_revised AS revised
		FROM dmz_doc_link
		WHERE c_orgid=? AND c_sourcedocid=? AND c_sourcesectionid=?`,
		ctx.OrgID, documentID, pageID)

	if err != nil && err != sql.ErrNoRows {
		err = errors.Wrap(err, "get page links")
		return
	}
	if len(links) == 0 {
		links = []link.Link{}
	}

	return
}

// MarkOrphanDocumentLink marks all link records referencing specified document.
func (s Scope) MarkOrphanDocumentLink(ctx domain.RequestContext, documentID string) (err error) {
	revised := time.Now().UTC()
	_, err = ctx.Transaction.Exec(`UPDATE dmz_doc_link SET
        c_orphan=1, c_revised=?
        WHERE c_type='document' AND c_orgid=? AND c_targetdocid=?`,
		revised, ctx.OrgID, documentID)

	if err != nil {
		err = errors.Wrap(err, "mark link as orphan")
	}

	return
}

// MarkOrphanPageLink marks all link records referencing specified page.
func (s Scope) MarkOrphanPageLink(ctx domain.RequestContext, pageID string) (err error) {
	revised := time.Now().UTC()
	_, err = ctx.Transaction.Exec(`UPDATE dmz_doc_link SET
        c_orphan=1, c_revised=?
        WHERE c_type='section' AND c_orgid=? AND c_targetid=?`,
		revised, ctx.OrgID, pageID)

	if err != nil {
		err = errors.Wrap(err, "mark orphan page link")
	}

	return
}

// MarkOrphanAttachmentLink marks all link records referencing specified attachment.
func (s Scope) MarkOrphanAttachmentLink(ctx domain.RequestContext, attachmentID string) (err error) {
	revised := time.Now().UTC()
	_, err = ctx.Transaction.Exec(`UPDATE dmz_doc_link SET
        c_orphan=1, c_revised=?
        WHERE c_type='file' AND c_orgid=? AND c_targetid=?`,
		revised, ctx.OrgID, attachmentID)

	if err != nil {
		err = errors.Wrap(err, "mark orphan attachment link")
	}

	return
}

// DeleteSourcePageLinks removes saved links for given source.
func (s Scope) DeleteSourcePageLinks(ctx domain.RequestContext, pageID string) (rows int64, err error) {
	b := mysql.BaseQuery{}
	return b.DeleteWhere(ctx.Transaction, fmt.Sprintf("DELETE FROM dmz_doc_link WHERE c_orgid=\"%s\" AND c_sourcesectionid=\"%s\"", ctx.OrgID, pageID))
}

// DeleteSourceDocumentLinks removes saved links for given document.
func (s Scope) DeleteSourceDocumentLinks(ctx domain.RequestContext, documentID string) (rows int64, err error) {
	b := mysql.BaseQuery{}
	return b.DeleteWhere(ctx.Transaction, fmt.Sprintf("DELETE FROM dmz_doc_link WHERE c_orgid=\"%s\" AND c_sourcedocid=\"%s\"", ctx.OrgID, documentID))
}

// DeleteLink removes saved link from the store.
func (s Scope) DeleteLink(ctx domain.RequestContext, id string) (rows int64, err error) {
	b := mysql.BaseQuery{}
	return b.DeleteConstrained(ctx.Transaction, "dmz_doc_link", ctx.OrgID, id)
}

// SearchCandidates returns matching documents, sections and attachments using keywords.
func (s Scope) SearchCandidates(ctx domain.RequestContext, keywords string) (docs []link.Candidate,
	pages []link.Candidate, attachments []link.Candidate, err error) {

	// find matching documents
	temp := []link.Candidate{}
	keywords = strings.TrimSpace(strings.ToLower(keywords))
	likeQuery := "LOWER(c_name) LIKE '%" + keywords + "%'"

	err = s.Runtime.Db.Select(&temp, `
		SELECT d.c_refid AS documentid, d.c_spaceid AS spaceid, d.c_name, l.c_name AS context
        FROM dmz_doc d LEFT JOIN dmz_space l ON d.c_spaceid=l.c_refid
        WHERE l.c_orgid=? AND `+likeQuery+` AND d.c_spaceid IN
		    (SELECT c_refid FROM dmz_space WHERE c_orgid=? AND c_refid IN
                (SELECT c_refid FROM dmz_permission WHERE c_orgid=? AND c_location='space' AND c_refid IN
                    (SELECT c_refid from dmz_permission WHERE c_orgid=? AND c_who='user' AND (c_whoid=? OR c_whoid='0') AND c_location='space' AND c_action='view'
				    UNION ALL
                    SELECT p.c_refid from dmz_permission p LEFT JOIN dmz_group_member r ON p.c_whoid=r.c_groupid WHERE p.c_orgid=? AND p.c_who='role'
                    AND p.c_location='space' AND p.c_action='view' AND (r.c_userid=? OR r.c_userid='0')
                    )
                )
            )
		ORDER BY title`, ctx.OrgID, ctx.OrgID, ctx.OrgID, ctx.OrgID, ctx.UserID, ctx.OrgID, ctx.UserID)

	if err != nil {
		err = errors.Wrap(err, "execute search links 1")
		return
	}

	for _, r := range temp {
		c := link.Candidate{
			RefID:      uniqueid.Generate(),
			SpaceID:    r.SpaceID,
			DocumentID: r.DocumentID,
			TargetID:   r.DocumentID,
			LinkType:   "document",
			Title:      r.Title,
			Context:    r.Context,
		}

		docs = append(docs, c)
	}

	// find matching sections
	likeQuery = "LOWER(p.c_name) LIKE '%" + keywords + "%'"
	temp = []link.Candidate{}

	err = s.Runtime.Db.Select(&temp, `
        SELECT p.c_refid AS targetid, p.c_docid AS documentid, p.c_name AS title,
        p.c_type AS linktype, d.c_name AS context, d.c_spaceid AS spaceid
        FROM dmz_section p LEFT JOIN dmz_doc d ON d.c_refid=p.c_docid
        WHERE p.c_orgid=? AND `+likeQuery+` AND d.c_spaceid IN
		    (SELECT c_refid FROM dmz_space WHERE c_orgid=? AND c_refid IN
                (SELECT c_refid FROM dmz_permission WHERE c_orgid=? AND c_location='space' AND c_refid IN
                    (SELECT c_refid from dmz_permission WHERE c_orgid=? AND c_who='user' AND (c_whoid=? OR c_whoid='0') AND c_location='space' AND c_action='view'
                    UNION ALL
                    SELECT p.c_refid from dmz_permission p LEFT JOIN dmz_group_member r ON p.c_whoid=r.c_groupid WHERE p.c_orgid=? AND p.c_who='role'
                    AND p.c_location='space' AND p.c_action='view' AND (r.c_userid=? OR r.c_userid='0')
                    )
                )
		    )
        ORDER BY p.c_name`,
		ctx.OrgID, ctx.OrgID, ctx.OrgID, ctx.OrgID, ctx.UserID, ctx.OrgID, ctx.UserID)

	if err != nil {
		err = errors.Wrap(err, "execute search links 2")
		return
	}

	for _, r := range temp {
		c := link.Candidate{
			RefID:      uniqueid.Generate(),
			SpaceID:    r.SpaceID,
			DocumentID: r.DocumentID,
			TargetID:   r.TargetID,
			LinkType:   r.LinkType,
			Title:      r.Title,
			Context:    r.Context,
		}

		pages = append(pages, c)
	}

	// find matching attachments
	likeQuery = "LOWER(a.filename) LIKE '%" + keywords + "%'"
	temp = []link.Candidate{}

	err = s.Runtime.Db.Select(&temp, `
        SELECT a.c_refid AS targetid, a.c_docid AS documentid, a.c_filename AS title, a.c_extension AS context, d.c_spaceiid AS spaceid
        FROM dmz_doc_attachment a LEFT JOIN dmz_doc d ON d.c_refid=a.c_docid
        WHERE a.c_orgid=? AND `+likeQuery+` AND d.c_spaceid IN
            (SELECT c_refid FROM dmz_space WHERE c_orgid=? AND c_refid IN
                (SELECT c_refid FROM dmz_permission WHERE c_orgid=? AND c_location='space' AND c_refid IN
                    (SELECT c_refid from dmz_permission WHERE c_orgid=? AND c_who='user' AND (c_whoid=? OR c_whoid='0') AND c_location='space' AND c_action='view'
                    UNION ALL
                    SELECT p.c_refid from dmz_permission p LEFT JOIN dmz_group_member r ON p.c_whoid=r.c_groupid WHERE p.c_orgid=? AND p.c_who='role'
                    AND p.c_location='space' AND p.c_action='view' AND (r.c_userid=? OR r.c_userid='0')
                    )
                )
		    )
		ORDER BY a.c_filename`, ctx.OrgID, ctx.OrgID, ctx.OrgID, ctx.OrgID, ctx.UserID, ctx.OrgID, ctx.UserID)

	if err != nil {
		err = errors.Wrap(err, "execute search links 3")
		return
	}

	for _, r := range temp {
		c := link.Candidate{
			RefID:      uniqueid.Generate(),
			SpaceID:    r.SpaceID,
			DocumentID: r.DocumentID,
			TargetID:   r.TargetID,
			LinkType:   "file",
			Title:      r.Title,
			Context:    r.Context,
		}

		attachments = append(attachments, c)
	}

	if len(docs) == 0 {
		docs = []link.Candidate{}
	}
	if len(pages) == 0 {
		pages = []link.Candidate{}
	}
	if len(attachments) == 0 {
		attachments = []link.Candidate{}
	}

	return
}
