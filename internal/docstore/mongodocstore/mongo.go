// Copyright 2019 The Go Cloud Development Kit Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package mongodocstore provides an implementation of the docstore API for MongoDB.
//
// URLs
//
// For docstore.OpenCollection, mongodocstore registers for the scheme "mongo".
// The default URL opener will dial a Mongo server using the environment
// variable "MONGO_SERVER_URL".
// To customize the URL opener, or for more details on the URL format,
// see URLOpener.
// See https://gocloud.dev/concepts/urls/ for background information.
//
//
// As
//
// mongodocstore exposes the following types for As:
// - Collection: *mongo.Collection
// - ActionList.BeforeDo: *options.FindOptions, *options.InsertOneOptions,
//   *options.ReplaceOptions, *options.UpdateOptions or *options.DeleteOptions
// - Query.BeforeQuery: *options.FindOptions
// - DocumentIterator: *mongo.Cursor
// - Error: mongo.CommandError, mongo.BulkWriteError, mongo.BulkWriteException
//
//
// Special Considerations
//
// MongoDB represents times to millisecond precision, while Go's time.Time type has
// nanosecond precision. To save time.Times to MongoDB without loss of precision,
// save the result of calling  UnixNano on the time.
//
// The official Go driver for MongoDB, go.mongodb.org/mongo-driver/mongo, lowercases
// struct field names; other docstore drivers do not. This means that you have to choose
// between interoperating with the MongoDB driver and interoperating with other docstore drivers.
// See Options.LowercaseFields for more information.
package mongodocstore // import "gocloud.dev/internal/docstore/mongodocstore"

// MongoDB reference manual: https://docs.mongodb.com/manual
// Client documentation: https://godoc.org/go.mongodb.org/mongo-driver/mongo
//
// The client methods accept a document of type interface{},
// which is marshaled by the go.mongodb.org/mongo-driver/bson package.

import (
	"context"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gocloud.dev/gcerrors"
	"gocloud.dev/internal/docstore"
	"gocloud.dev/internal/docstore/driver"
	"gocloud.dev/internal/gcerr"
)

// Dial returns a new mongoDB client that is connected to the server URI.
func Dial(ctx context.Context, uri string) (*mongo.Client, error) {
	opts := options.Client().ApplyURI(uri)
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	client, err := mongo.NewClient(opts)
	if err != nil {
		return nil, err
	}
	if err := client.Connect(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

type collection struct {
	coll          *mongo.Collection
	idField       string
	idFunc        func(docstore.Document) interface{}
	revisionField string
	opts          *Options
}

type Options struct {
	// Lowercase all field names for document encoding, field selection, update
	// modifications and queries.
	//
	// If false (the default), then struct fields and MongoDB document fields will
	// have the same names. For example, a struct field F will correspond to a
	// MongoDB document field "F". This setting matches the behavior of other
	// docstore providers, making code portable across providers.
	//
	// If true, struct fields correspond to lower-cased MongoDB document fields. The
	// struct field F will correspond to the MongoDB document field "f", for
	// instance. Use this to make code that uses this package interoperate with code
	// that uses the official Go client for MongoDB,
	// go.mongodb.org/mongo-driver/mongo, which lowercases field names.
	LowercaseFields bool
}

// OpenCollection opens a MongoDB collection for use with Docstore.
// The idField argument is the name of the document field to use for the document ID
// (MongoDB's _id field). If it is empty, the field "_id" will be used.
func OpenCollection(mcoll *mongo.Collection, idField string, opts *Options) (*docstore.Collection, error) {
	dc, err := newCollection(mcoll, idField, nil, opts)
	if err != nil {
		return nil, err
	}
	return docstore.NewCollection(dc), nil
}

// OpenCollectionWithIDFunc opens a MongoDB collection for use with Docstore.
// The idFunc argument is function that accepts a document and returns the value to
// be used for the document ID (MongoDB's _id field). IDFunc should return nil if the
// document is missing the information to construct an ID. This will cause all
// actions, even Create, to fail.
func OpenCollectionWithIDFunc(mcoll *mongo.Collection, idFunc func(docstore.Document) interface{}, opts *Options) (*docstore.Collection, error) {
	dc, err := newCollection(mcoll, "", idFunc, opts)
	if err != nil {
		return nil, err
	}
	return docstore.NewCollection(dc), nil
}

func newCollection(mcoll *mongo.Collection, idField string, idFunc func(docstore.Document) interface{}, opts *Options) (*collection, error) {
	if opts == nil {
		opts = &Options{}
	}
	c := &collection{
		coll:          mcoll,
		idField:       idField,
		idFunc:        idFunc,
		revisionField: docstore.RevisionField,
		opts:          opts,
	}
	if c.idField == "" && c.idFunc == nil {
		c.idField = mongoIDField
	}

	if opts.LowercaseFields {
		c.idField = strings.ToLower(c.idField)
		c.revisionField = strings.ToLower(c.revisionField)
	}
	return c, nil
}

func (c *collection) Key(doc driver.Document) (interface{}, error) {
	if c.idField != "" {
		id, _ := doc.GetField(c.idField)
		return id, nil // missing field is not an error
	}
	id := c.idFunc(doc.Origin)
	if id == nil {
		return nil, gcerr.Newf(gcerr.InvalidArgument, nil, "missing document key")
	}
	return id, nil
}

func (c *collection) RevisionField() string {
	// TODO(jba): should this be the lowercased version?
	return c.revisionField
}

// From https://docs.mongodb.com/manual/core/document: "The field name _id is
// reserved for use as a primary key; its value must be unique in the collection, is
// immutable, and may be of any type other than an array."
const mongoIDField = "_id"

func (c *collection) RunActions(ctx context.Context, actions []*driver.Action, opts *driver.RunActionsOptions) driver.ActionListError {
	errs := make([]error, len(actions))
	beforeGets, gets, writes, afterGets := driver.GroupActions(actions)
	c.runGets(ctx, beforeGets, errs, opts)
	ch := make(chan []error)
	go func() { ch <- c.bulkWrite(ctx, writes, errs) }()
	c.runGets(ctx, gets, errs, opts)
	writeErrs := <-ch
	c.runGets(ctx, afterGets, errs, opts)
	alerr := driver.NewActionListError(errs)
	for _, werr := range writeErrs {
		alerr = append(alerr, indexedError{-1, werr})
	}
	return alerr
}

type indexedError = struct {
	Index int
	Err   error
}

func (c *collection) runGets(ctx context.Context, gets []*driver.Action, errs []error, opts *driver.RunActionsOptions) {
	// TODO(shantuo): figure out a reasonable batch size, there is no hard limit on
	// the item number or filter string length. The limit for bulk write batch size
	// is 100,000.
	for _, group := range driver.GroupByFieldPath(gets) {
		c.bulkFind(ctx, group, errs, opts)
	}
}

func (c *collection) bulkFind(ctx context.Context, gets []*driver.Action, errs []error, dopts *driver.RunActionsOptions) {
	// errors need to be mapped to the actions' indices.
	setErr := func(err error) {
		for _, get := range gets {
			if errs[get.Index] == nil {
				errs[get.Index] = err
			}
		}
	}

	opts := options.Find()
	if len(gets[0].FieldPaths) > 0 {
		opts.Projection = c.projectionDoc(gets[0].FieldPaths)
	}
	ids := bson.A{}
	idToAction := map[interface{}]*driver.Action{}
	for _, a := range gets {
		id, err := encodeValue(a.Key)
		if err != nil {
			errs[a.Index] = err
		} else {
			ids = append(ids, id)
			idToAction[id] = a
		}
	}
	if dopts.BeforeDo != nil {
		if err := dopts.BeforeDo(driver.AsFunc(opts)); err != nil {
			setErr(err)
			return
		}
	}
	cursor, err := c.coll.Find(ctx, bson.D{bson.E{Key: mongoIDField, Value: bson.D{{Key: "$in", Value: ids}}}}, opts)
	if err != nil {
		setErr(err)
		return
	}
	defer cursor.Close(ctx)

	found := make(map[*driver.Action]bool)
	for cursor.Next(ctx) {
		var m map[string]interface{}
		if err := cursor.Decode(&m); err != nil {
			continue
		}
		a := idToAction[m[mongoIDField]]
		errs[a.Index] = decodeDoc(m, a.Doc, c.idField)
		found[a] = true
	}
	for _, a := range gets {
		if !found[a] {
			errs[a.Index] = gcerr.Newf(gcerr.NotFound, nil, "item with key %v not found", a.Key)
		}
	}
}

// Construct a mongo "projection document" from field paths.
// Always include the revision field.
func (c *collection) projectionDoc(fps [][]string) bson.D {
	proj := bson.D{{Key: c.revisionField, Value: 1}}
	for _, fp := range fps {
		proj = append(proj, bson.E{Key: c.toMongoFieldPath(fp), Value: 1})
	}
	return proj
}

func (c *collection) toMongoFieldPath(fp []string) string {
	if c.opts.LowercaseFields {
		sliceToLower(fp)
	}
	return strings.Join(fp, ".")
}

func sliceToLower(s []string) {
	for i, e := range s {
		s[i] = strings.ToLower(e)
	}
}

func (c *collection) prepareCreate(a *driver.Action) (mdoc, createdID interface{}, rev string, err error) {
	id := a.Key
	if id == nil {
		// Create a unique ID here. (The MongoDB Go client does this for us when calling InsertOne,
		// but not for BulkWrite.)
		id = primitive.NewObjectID()
		createdID = id
	} else {
		id, err = encodeValue(id)
		if err != nil {
			return nil, nil, "", err
		}
	}
	mdoc, rev, err = c.encodeDoc(a.Doc, id)
	if err != nil {
		return nil, nil, "", err
	}
	return mdoc, createdID, rev, nil
}

func (c *collection) prepareReplace(a *driver.Action) (filter bson.D, mdoc map[string]interface{}, rev string, err error) {
	id, err := encodeValue(a.Key)
	if err != nil {
		return nil, nil, "", err
	}
	filter, _, err = c.makeFilter(id, a.Doc)
	if err != nil {
		return nil, nil, "", err
	}
	mdoc, rev, err = c.encodeDoc(a.Doc, id)
	if err != nil {
		return nil, nil, "", err
	}
	return filter, mdoc, rev, nil
}

// encodeDoc encodes doc and sets its ID to the encoded value id. It also creates a new revision and sets it.
// It returns the encoded document and the new revision.
func (c *collection) encodeDoc(doc driver.Document, id interface{}) (map[string]interface{}, string, error) {
	mdoc, err := encodeDoc(doc, c.opts.LowercaseFields)
	if err != nil {
		return nil, "", err
	}
	if id != nil {
		if c.idField != "" {
			delete(mdoc, c.idField)
		}
		mdoc[mongoIDField] = id
	}
	rev := driver.UniqueString()
	mdoc[c.revisionField] = rev
	return mdoc, rev, nil
}

func (c *collection) prepareUpdate(a *driver.Action) (filter bson.D, updateDoc map[string]bson.D, rev string, err error) {
	id, err := encodeValue(a.Key)
	if err != nil {
		return nil, nil, "", err
	}
	filter, _, err = c.makeFilter(id, a.Doc)
	if err != nil {
		return nil, nil, "", err
	}
	updateDoc, rev, err = c.newUpdateDoc(a.Mods)
	if err != nil {
		return nil, nil, "", err
	}
	return filter, updateDoc, rev, nil
}

func (c *collection) newUpdateDoc(mods []driver.Mod) (map[string]bson.D, string, error) {
	var (
		sets   bson.D
		unsets bson.D
		incs   bson.D
	)
	for _, m := range mods {
		key := c.toMongoFieldPath(m.FieldPath)
		if m.Value == nil {
			unsets = append(unsets, bson.E{Key: key, Value: ""})
		} else if inc, ok := m.Value.(driver.IncOp); ok {
			val, err := encodeValue(inc.Amount)
			if err != nil {
				return nil, "", err
			}
			incs = append(incs, bson.E{Key: key, Value: val})
		} else {
			val, err := encodeValue(m.Value)
			if err != nil {
				return nil, "", err
			}
			sets = append(sets, bson.E{Key: key, Value: val})
		}
	}
	updateDoc := map[string]bson.D{}
	rev := driver.UniqueString()
	updateDoc["$set"] = append(sets, bson.E{Key: c.revisionField, Value: rev})
	if len(unsets) > 0 {
		updateDoc["$unset"] = unsets
	}
	if len(incs) > 0 {
		updateDoc["$inc"] = incs
	}
	return updateDoc, rev, nil
}

// makeFilter constructs a filter using the given encoded id and the document's revision field, if any.
func (c *collection) makeFilter(id interface{}, doc driver.Document) (filter bson.D, rev interface{}, err error) {
	rev, err = doc.GetField(c.revisionField)
	if err != nil && gcerrors.Code(err) != gcerrors.NotFound {
		return nil, nil, err
	}
	// Only select the document with the given ID.
	filter = bson.D{bson.E{Key: "_id", Value: id}}
	// If the given document has a revision, it must match the stored document.
	if rev != nil {
		filter = append(filter, bson.E{Key: c.revisionField, Value: rev})
	}
	return filter, rev, nil
}

// bulkWrite calls the Mongo driver's BulkWrite RPC in unordered mode with the
// actions, which must be writes.
// errs is the slice of errors indexed by the position of the action in the original
// action list. bulkWrite populates this slice. In addition, bulkWrite returns a list
// of errors that cannot be attributed to any single action.
func (c *collection) bulkWrite(ctx context.Context, actions []*driver.Action, errs []error) []error {
	var (
		models          []mongo.WriteModel
		modelActions    []*driver.Action // corresponding action for each model
		newIDs          []interface{}    // new IDs for Create actions, corresponding to models slice
		revs            []string         // new revisions, corresponding to models slice
		nDeletes        int64
		nNonCreateWrite int64 // total operations expected from Put, Replace and Update
	)
	for _, a := range actions {
		var m mongo.WriteModel
		var err error
		var newID interface{}
		var rev string
		switch a.Kind {
		case driver.Create:
			m, newID, rev, err = c.newCreateModel(a)
		case driver.Delete:
			m, err = c.newDeleteModel(a)
			if err == nil {
				nDeletes++
			}
		case driver.Replace, driver.Put:
			m, rev, err = c.newReplaceModel(a, a.Kind == driver.Put)
			if err == nil {
				nNonCreateWrite++
			}
		case driver.Update:
			m, rev, err = c.newUpdateModel(a)
			if err == nil && m != nil {
				nNonCreateWrite++
			}
		default:
			err = gcerr.Newf(gcerr.Internal, nil, "bad action %+v", a)
		}
		if err != nil {
			errs[a.Index] = err
		} else if m != nil { // m can be nil for a no-op update
			models = append(models, m)
			modelActions = append(modelActions, a)
			newIDs = append(newIDs, newID)
			revs = append(revs, rev)
		}
	}
	if len(models) == 0 {
		return nil
	}
	// TODO(jba): improve independent execution. I think that even if BulkWrite returns an error,
	// some of the actions may have succeeded.
	var reterrs []error
	res, err := c.coll.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	if err != nil {
		bwe, ok := err.(mongo.BulkWriteException)
		if !ok { // assume everything failed with this error
			return []error{err}
		}
		// The returned indexes of the WriteErrors are wrong. See https://jira.mongodb.org/browse/GODRIVER-1028.
		// Until it's fixed, use negative values for the indexes in the errors we return.
		for _, w := range bwe.WriteErrors {
			reterrs = append(reterrs, gcerr.Newf(translateMongoCode(w.Code), w, "%s", w.Message))
		}
		return reterrs
	}
	for i, newID := range newIDs {
		if newID == nil {
			continue
		}
		a := modelActions[i]
		if err := a.Doc.SetField(c.idField, newID); err != nil {
			errs[a.Index] = err
		}
	}
	for i, rev := range revs {
		if rev != "" {
			// Ignore error, because document may not have a revision field.
			_ = modelActions[i].Doc.SetField(c.revisionField, rev)
		}
	}
	if res.DeletedCount != nDeletes {
		// Some Delete actions failed. It's not an error if a Delete failed because
		// the document didn't exist, but it is an error if it failed because of a
		// precondition mismatch. Find all the documents with revisions we tried to delete; if
		// any are still present, that's an error.
		c.determineDeleteErrors(ctx, models, modelActions, errs)
	}
	if res.MatchedCount+res.UpsertedCount != nNonCreateWrite {
		reterrs = append(reterrs, gcerr.Newf(gcerr.NotFound, nil, "some writes failed (replaced %d, upserted %d, out of total %d)", res.MatchedCount, res.UpsertedCount, nNonCreateWrite))
	}
	return reterrs
}

func (c *collection) determineDeleteErrors(ctx context.Context, models []mongo.WriteModel, actions []*driver.Action, errs []error) {
	// TODO(jba): do this concurrently.
	for i, m := range models {
		if dm, ok := m.(*mongo.DeleteOneModel); ok {
			filter := dm.Filter.(bson.D)
			if len(filter) > 1 {
				// Delete with both ID and revision. See if the document is still there.
				idOnlyFilter := filter[:1]
				// TODO(shantuo): use Find instead of FindOne.
				res := c.coll.FindOne(ctx, idOnlyFilter)

				// Assume an error means the document wasn't found.
				// That means either that it was deleted successfully, or that it never
				// existed. Either way, it's not an error.
				// TODO(jba): distinguish between not found and other errors.
				if res.Err() == nil {
					// The document exists, but we didn't delete it: assume we had the wrong
					// revision.
					errs[actions[i].Index] = gcerr.Newf(gcerr.FailedPrecondition, nil,
						"wrong revision for document with ID %v", actions[i].Key)
				}
			}
		}
	}
}

func (c *collection) newCreateModel(a *driver.Action) (*mongo.InsertOneModel, interface{}, string, error) {
	mdoc, createdID, rev, err := c.prepareCreate(a)
	if err != nil {
		return nil, nil, "", err
	}
	return &mongo.InsertOneModel{Document: mdoc}, createdID, rev, nil
}

func (c *collection) newDeleteModel(a *driver.Action) (*mongo.DeleteOneModel, error) {
	id, err := encodeValue(a.Key)
	if err != nil {
		return nil, err
	}
	filter, _, err := c.makeFilter(id, a.Doc)
	if err != nil {
		return nil, err
	}
	return &mongo.DeleteOneModel{Filter: filter}, nil
}

func (c *collection) newReplaceModel(a *driver.Action, upsert bool) (*mongo.ReplaceOneModel, string, error) {
	filter, mdoc, rev, err := c.prepareReplace(a)
	if err != nil {
		return nil, "", err
	}
	return &mongo.ReplaceOneModel{
		Filter:      filter,
		Replacement: mdoc,
		Upsert:      &upsert,
	}, rev, nil
}

func (c *collection) newUpdateModel(a *driver.Action) (*mongo.UpdateOneModel, string, error) {
	filter, updateDoc, rev, err := c.prepareUpdate(a)
	if err != nil {
		return nil, "", err
	}
	if filter == nil { // no-op
		return nil, "", nil
	}
	return &mongo.UpdateOneModel{Filter: filter, Update: updateDoc}, rev, nil
}

// As implements driver.As.
func (c *collection) As(i interface{}) bool {
	p, ok := i.(**mongo.Collection)
	if !ok {
		return false
	}
	*p = c.coll
	return true
}

// ErrorAs implements driver.Collection.ErrorAs
func (c *collection) ErrorAs(err error, i interface{}) bool {
	switch e := err.(type) {
	case mongo.CommandError:
		if p, ok := i.(*mongo.CommandError); ok {
			*p = e
			return true
		}
	case mongo.BulkWriteError:
		if p, ok := i.(*mongo.BulkWriteError); ok {
			*p = e
			return true
		}
	case mongo.BulkWriteException:
		if p, ok := i.(*mongo.BulkWriteException); ok {
			*p = e
			return true
		}
	}
	return false
}

func (c *collection) ErrorCode(err error) gcerrors.ErrorCode {
	if g, ok := err.(*gcerr.Error); ok {
		return g.Code
	}
	if err == mongo.ErrNoDocuments {
		return gcerrors.NotFound
	}
	if wexc, ok := err.(mongo.WriteException); ok && len(wexc.WriteErrors) > 0 {
		return translateMongoCode(wexc.WriteErrors[0].Code)
	}
	return gcerrors.Unknown
}

// Error code for a write error when no documents match a filter.
// (The Go mongo driver doesn't define an exported constant for this.)
const mongoDupKeyCode = 11000

func translateMongoCode(code int) gcerrors.ErrorCode {
	switch code {
	case mongoDupKeyCode:
		return gcerrors.AlreadyExists
	default:
		return gcerrors.Unknown
	}
}
