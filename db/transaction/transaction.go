// Copyright (C) 2015 NTT Innovation Institute, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package transaction

import (
	"context"
	"errors"

	"github.com/cloudwan/gohan/db/pagination"
	"github.com/cloudwan/gohan/schema"
	"github.com/jmoiron/sqlx"
)

// ErrResourceNotFound is error message for missing resource
var ErrResourceNotFound = errors.New("resource not found")

//Type represents transaction types
type Type string

const (
	//ReadUncommitted is transaction type for READ UNCOMMITTED
	//You don't need to use this for most case
	ReadUncommitted Type = "READ UNCOMMITTED"
	//ReadCommited is transaction type for READ COMMITTED
	//You don't need to use this for most case
	ReadCommited Type = "READ COMMITTED"
	//RepeatableRead is transaction type for REPEATABLE READ
	//This is default value for read request
	RepeatableRead Type = "REPEATABLE READ"
	//Serializable is transaction type for Serializable
	Serializable Type = "SERIALIZABLE"
)

type TxParams struct {
	Context        context.Context
	IsolationLevel Type
	TraceID        string
}

type Option func(*TxParams)

func NewTxParams(options ...Option) *TxParams {
	params := &TxParams{
		Context:        context.Background(),
		IsolationLevel: RepeatableRead,
	}

	for _, option := range options {
		option(params)
	}

	return params
}

func Context(ctx context.Context) Option {
	return func(params *TxParams) {
		params.Context = ctx
	}
}

func IsolationLevel(level Type) Option {
	return func(params *TxParams) {
		params.IsolationLevel = level
	}
}

func TraceId(traceId string) Option {
	return func(params *TxParams) {
		params.TraceID = traceId
	}
}

//Filter represents db filter
type Filter map[string]interface{}

//ResourceState represents the state of a resource
type ResourceState struct {
	ID            string `db:"id"`
	ConfigVersion int64  `db:"config_version"`
	StateVersion  int64  `db:"state_version"`
	Error         string `db:"state_error"`
	State         string `db:"state"`
	Monitoring    string `db:"state_monitoring"`
}

//ViewOptions specifies additional options.
type ViewOptions struct {
	// Details specifies if all the underlying structures should be
	// returned.
	Details bool
	// Fields limits list output to only showing selected fields.
	Fields []string
}

// A Result summarizes an executed SQL command.
type Result interface {
	// LastInsertId returns the integer generated by the database
	// in response to a command. Typically this will be from an
	// "auto increment" column when inserting a new row.
	LastInsertId() (int64, error)
}

//Transaction is common interface for handling transaction
type Transaction interface {
	RawTransaction() *sqlx.Tx
	Commit() error
	Close() error
	Closed() bool
	GetIsolationLevel() Type

	Create(context.Context, *schema.Resource) (Result, error)
	Update(context.Context, *schema.Resource) error
	StateUpdate(context.Context, *schema.Resource, *ResourceState) error
	Delete(context.Context, *schema.Schema, interface{}) error
	DeleteFilter(context.Context, *schema.Schema, Filter) error
	Fetch(context.Context, *schema.Schema, Filter, *ViewOptions) (*schema.Resource, error)
	LockFetch(context.Context, *schema.Schema, Filter, schema.LockPolicy, *ViewOptions) (*schema.Resource, error)
	StateFetch(context.Context, *schema.Schema, Filter) (ResourceState, error)
	StateList(ctx context.Context, s *schema.Schema, filter Filter) ([]ResourceState, error)
	List(context.Context, *schema.Schema, Filter, *ViewOptions, *pagination.Paginator) ([]*schema.Resource, uint64, error)
	LockList(context.Context, *schema.Schema, Filter, *ViewOptions, *pagination.Paginator, schema.LockPolicy) ([]*schema.Resource, uint64, error)
	Count(context.Context, *schema.Schema, Filter) (uint64, error)
	Query(context.Context, *schema.Schema, string, []interface{}) (list []*schema.Resource, err error)
	Exec(ctx context.Context, query string, args ...interface{}) error
}

// GetIsolationLevel returns isolation level for an action
func GetIsolationLevel(s *schema.Schema, action string) Type {
	level, ok := s.IsolationLevel[action]
	if !ok {
		switch action {
		case "read":
			return RepeatableRead
		default:
			return Serializable
		}
	}
	levelStr := level.(string)
	return Type(levelStr)
}

//IDFilter create filter for specific ID
func IDFilter(ID interface{}) Filter {
	return Filter{"id": ID}
}
