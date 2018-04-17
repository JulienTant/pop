package pop

import (
	"database/sql"
	"reflect"

	"github.com/gobuffalo/pop/associations"
	"github.com/pkg/errors"
)

// eagerMode specifies the way associations are loaded from database.
type eagerMode uint8

const (
	// EagerDefault the default implementation for Eager. it uses N+1 approach.
	EagerDefault eagerMode = iota
	// EagerCache allows to optimize queries. It eliminates N+1 problem with
	// a higher memory use cost.
	EagerCache
	// not specified, it is used as a nil value for queries. Every Query instance
	// it is initialized with this eager mode. See Q function in query.go
	eagerNotSpecified
)

// mode keeps current eager loading approach indicator. By default
// it uses EagerDefault.
var emode = EagerDefault

// SetEagerMode allows to change current associations loading
// approach from database for all queries.
func SetEagerMode(m eagerMode) {
	emode = m
}

// SetEagerMode allows to change current associations loading
// approach for a specific Connection instance.
func (c *Connection) SetEagerMode(m eagerMode) *Query {
	return Q(c).SetEagerMode(m)
}

// SetEagerMode allows to change current associations loading
// approach for a specific query instance.
func (q *Query) SetEagerMode(m eagerMode) *Query {
	q.eagerMode = m
	return q
}

func (q *Query) eagerLoad(model interface{}) error {
	if q.eagerMode == eagerNotSpecified {
		q.eagerMode = emode
	}
	switch q.eagerMode {
	case EagerDefault:
		return q.eagerLoadDefault(model)
	case EagerCache:
		return q.eagerLoadCache(model)
	default:
		return errors.Errorf("eager mode %v is not supported by pop", emode)
	}
}

func (q *Query) eagerLoadDefault(model interface{}) error {
	var err error

	// eagerAssociations for a slice or array model passed as a param.
	v := reflect.ValueOf(model)
	if reflect.Indirect(v).Kind() == reflect.Slice ||
		reflect.Indirect(v).Kind() == reflect.Array {
		v = v.Elem()
		for i := 0; i < v.Len(); i++ {
			err = q.eagerLoadDefault(v.Index(i).Addr().Interface())
			if err != nil {
				return err
			}
		}
		return err
	}

	assos, err := associations.AssociationsForStruct(model, q.eagerFields...)

	if err != nil {
		return err
	}

	for _, association := range assos {
		if association.Skipped() {
			continue
		}

		query := Q(q.Connection)
		query.eager = false

		whereCondition, args := association.Constraint()
		query = query.Where(whereCondition, args...)

		// validates if association is Sortable
		sortable := (*associations.AssociationSortable)(nil)
		t := reflect.TypeOf(association)
		if t.Implements(reflect.TypeOf(sortable).Elem()) {
			m := reflect.ValueOf(association).MethodByName("OrderBy")
			out := m.Call([]reflect.Value{})
			orderClause := out[0].String()
			if orderClause != "" {
				query = query.Order(orderClause)
			}
		}

		sqlSentence, args := query.ToSQL(&Model{Value: association.Interface()})
		query = query.RawQuery(sqlSentence, args...)

		if association.Kind() == reflect.Slice || association.Kind() == reflect.Array {
			err = query.All(association.Interface())
		}

		if association.Kind() == reflect.Struct {
			err = query.First(association.Interface())
		}

		if err != nil && errors.Cause(err) != sql.ErrNoRows {
			return err
		}

		// load all inner associations.
		innerAssociations := association.InnerAssociations()
		for _, inner := range innerAssociations {
			v = reflect.Indirect(reflect.ValueOf(model)).FieldByName(inner.Name)
			q.eagerFields = []string{inner.Fields}
			err = q.eagerLoadDefault(v.Addr().Interface())
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (q *Query) eagerLoadCache(model interface{}) error {
	return nil
}
