/*
 *    Copyright 2023 Stephen Guo
 *
 *    Licensed under the Apache License, Version 2.0 (the "License");
 *    you may not use this file except in compliance with the License.
 *    You may obtain a copy of the License at
 *
 *        http://www.apache.org/licenses/LICENSE-2.0
 *
 *    Unless required by applicable law or agreed to in writing, software
 *    distributed under the License is distributed on an "AS IS" BASIS,
 *    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *    See the License for the specific language governing permissions and
 *    limitations under the License.
 *
 */

package dfpt

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
)

type Traveller struct {
	adapter         reflect.Value
	conf            *TraverseConf
	nilPtrMethod    reflect.Value                  // nilPtrMethod.IsValid means has a ForNilPtr binding function
	typeMethods     map[reflect.Type]reflect.Value // type -> method
	kindMethods     map[reflect.Kind]reflect.Value // kind -> method
	typeOrder       orderItems                     // all type list in order (tag order or declare order)
	structTypeCache sync.Map
}

func NewTraveller(adapter interface{}, config ...*TraverseConf) (*Traveller, error) {
	aptVal := reflect.ValueOf(adapter)
	if !aptVal.IsValid() {
		return nil, ErrInvalidAdapter
	}
	aptType := aptVal.Type()
	var items orderItems
	var nilPtrMethod reflect.Value
	typeMethods := make(map[reflect.Type]reflect.Value)
	kindMethods := make(map[reflect.Kind]reflect.Value)
	for i := 0; i < aptType.NumMethod(); i++ {
		m := aptType.Method(i)
		itype, inKind, ok := Unknown.Which(m.Name)
		if !ok {
			continue
		}
		if !itype.IsValidWithReceiver(m) {
			continue
		}
		fType := m.Func.Type()
		switch itype {
		case ForImpl, ForAssign:
			inType := fType.In(itype.ParamLength())
			if _, exist := typeMethods[inType]; exist {
				return nil, fmt.Errorf("duplicated binding function %s found for Type:%s", m.Name, inType.Name())
			}
			items = append(items, orderItem{
				i: i,
				n: m.Name,
				o: 0,
				t: inType,
				c: false, // there's no possibility of further in-depth analysis with explicit type binding
				k: reflect.Invalid,
			})
			typeMethods[inType] = aptVal.Method(i)
		case ForKind, ForContainer:
			if _, exist := kindMethods[inKind]; exist {
				return nil, fmt.Errorf("duplicated binding function %s found for Kind:%s", m.Name, inKind.String())
			}
			items = append(items, orderItem{
				i: i,
				n: m.Name,
				o: 0,
				t: nil,
				c: itype == ForContainer,
				k: inKind,
			})
			kindMethods[inKind] = aptVal.Method(i)
		case ForNilPtr:
			if nilPtrMethod.IsValid() {
				return nil, fmt.Errorf("duplicated binding function %s found for Nil Ptr", m.Name)
			}
			nilPtrMethod = aptVal.Method(i)
		}
	}
	if len(items) == 0 {
		return nil, errors.New("no available binding function found")
	}
	sort.Sort(items)
	var conf *TraverseConf
	if len(config) > 0 && config[0] != nil {
		conf = config[0].Clone()
	}
	return &Traveller{
		adapter:      aptVal,
		conf:         conf,
		nilPtrMethod: nilPtrMethod,
		typeMethods:  typeMethods,
		kindMethods:  kindMethods,
		typeOrder:    items,
	}, nil
}

func (t *Traveller) String() string {
	if t == nil {
		return "Traveller<nil>"
	}
	adapterStr := ""
	if !t.adapter.IsValid() {
		adapterStr = "adapter:Invalid"
	} else {
		typ := t.adapter.Type()
		adapterStr = fmt.Sprintf("adapter:%s", typ.Name())
	}
	if t.nilPtrMethod.IsValid() {
		return fmt.Sprintf("Traveller{%s NilPtr Types:%d Kinds:%d Items:%s}",
			adapterStr, len(t.typeMethods), len(t.kindMethods), []orderItem(t.typeOrder))
	}
	return fmt.Sprintf("Traveller{%s Types:%d Kinds:%d Items:%s}",
		adapterStr, len(t.typeMethods), len(t.kindMethods), []orderItem(t.typeOrder))
}

func (t *Traveller) _call(parent *parentInfo, val reflect.Value) (goin, reEnter bool, info *parentInfo, newVal reflect.Value, err error) {
	if !val.IsValid() {
		return false, false, nil, reflect.Value{}, errors.New("invalid value")
	}
	if t.nilPtrMethod.IsValid() && val.Type().Kind() == reflect.Ptr && val.IsNil() {
		outs := t.nilPtrMethod.Call(parent.callIns(val))
		_, err = ForNilPtr.parseReturns(outs)
		return false, false, nil, reflect.Value{}, err
	}
	for i, item := range t.typeOrder {
		itype, typ, kind, match := item.match(val)
		if !match {
			continue
		}
		var outs []reflect.Value
		if typ != nil {
			fVal, ok := t.typeMethods[typ]
			if !ok || !fVal.IsValid() {
				panic(fmt.Errorf("matching %d item %s, but function not found by Type:%s", i, item, typ.Name()))
			}
			outs = fVal.Call(parent.callIns(val))
		} else if kind != reflect.Invalid {
			fVal, ok := t.kindMethods[kind]
			if !ok || !fVal.IsValid() {
				panic(fmt.Errorf("matching %d item %s, but function not found by Kind:%s", i, item, kind.String()))
			}
			if _, isContainer := _containers[kind]; isContainer {
				var size int
				var fields []Property
				switch kind {
				case reflect.Array:
					size = val.Len()
				case reflect.Slice:
					if !val.IsNil() {
						size = val.Len()
					}
				case reflect.Map:
					if !val.IsNil() {
						size = val.Len() << 1
					}
				case reflect.Struct:
					size, fields = t._structProperties(val)
				case reflect.Ptr:
					if !val.IsNil() {
						size = 1
					}
				}
				info = &parentInfo{
					depth:        parent.nextDepth(),
					value:        val,
					size:         size,
					offset:       -1,
					structFields: fields,
					binding:      fVal,
				}
				outs = fVal.Call(parent.startContainerIns(info, val))
			} else {
				outs = fVal.Call(parent.callIns(val))
			}
		} else {
			panic(fmt.Errorf("SHOULD NOT BE HERE!! matching %d item %s, Kind:%s", i, item, kind.String()))
		}
		goin, err = itype.parseReturns(outs)
		if err != nil {
			return false, false, nil, reflect.Value{}, err
		}
		return goin, false, info, reflect.Value{}, nil
	}
	if t.conf != nil && t.conf.PtrAutoGoIn {
		if val.Type().Kind() == reflect.Ptr {
			if val.IsNil() == false {
				newVal = val.Elem()
				return false, true, parent, newVal, nil
			} else {
				return false, false, parent, reflect.Value{}, nil
			}
		}
	}
	if t.conf == nil || !t.conf.IgnoreMissedBinding {
		return false, false, nil, reflect.Value{},
			fmt.Errorf("type:%s kind:%s binding is missing", val.Type(), val.Type().Kind())
	}
	return false, false, nil, reflect.Value{}, nil
}

func (t *Traveller) _structProperties(val reflect.Value) (int, []Property) {
	if !val.IsValid() {
		return 0, nil
	}
	if t.conf != nil && t.conf.Propertier != nil {
		return t.conf.Propertier.Properties(val)
	}
	var ps []Property
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		if f := typ.Field(i); f.PkgPath == "" {
			ps = append(ps, Property{
				Index:        i,
				Name:         f.Name,
				IndexForReal: -1,
			})
		}
	}
	return len(ps), ps
}

func (t *Traveller) _traverse(parent *parentInfo, val reflect.Value) error {
	if !val.IsValid() {
		return fmt.Errorf("invalid value in _traverse(parent:%s, val:%s)", parent, val.String())
	}
	var next *parentInfo
	var goin, reEnter bool
	var err error
	oldVal := val
	var newVal reflect.Value
	for {
		goin, reEnter, next, newVal, err = t._call(parent, oldVal)
		if err != nil {
			return err
		}
		if reEnter {
			if !newVal.IsValid() {
				panic(fmt.Errorf("reenter need a valid value, oldVal:%s", oldVal))
			}
			oldVal = newVal
			continue
		}
		if !goin {
			return nil
		}
		if next == nil {
			panic(fmt.Errorf("container value need next *parentInfo, parent:%s val:%s", parent, oldVal.String()))
		}
		break
	}
	switch oldVal.Kind() {
	case reflect.Array, reflect.Slice:
		for i := 0; i < next.size; i++ {
			child := oldVal.Index(i)
			next.offset = i
			if err = t._traverse(next, child); err != nil {
				return err
			}
		}
	case reflect.Map:
		if next.size > 0 {
			keys := oldVal.MapKeys()
			if len(keys)<<1 != next.size {
				panic(fmt.Errorf("next:%s but len(keys)==%d", next, len(keys)))
			}
			for i := 0; i < len(keys); i++ {
				// stack value for map: idx%2==0 is the key of map, idx%2==1 is the value of map
				next.offset = i << 1
				if err = t._traverse(next, keys[i]); err != nil {
					return err
				}
				value := oldVal.MapIndex(keys[i])
				next.offset = i<<1 + 1
				if err = t._traverse(next, value); err != nil {
					return err
				}
			}
		}
	case reflect.Struct:
		for i := 0; i < len(next.structFields); i++ {
			field := next.structFields[i]
			if field.Index < 0 {
				continue
			}
			fieldVal := oldVal.Field(field.Index)
			next.offset = i
			if err = t._traverse(next, fieldVal); err != nil {
				return err
			}
		}
	case reflect.Ptr:
		if next.size > 0 {
			elem := oldVal.Elem()
			next.offset = 0
			if err = t._traverse(next, elem); err != nil {
				return err
			}
		}
	default:
		panic("unknown status")
	}
	if t.conf != nil && t.conf.ContainerEnd {
		outs := next.binding.Call(parent.endContainerIns(next, oldVal))
		_, err = ForContainer.parseReturns(outs)
		if err != nil {
			return fmt.Errorf("call container end failed: %v", err)
		}
	}
	return nil
}

func (t *Traveller) Traverse(obj interface{}) error {
	val := reflect.ValueOf(obj)
	if !val.IsValid() {
		return nil
	}
	return t._traverse(nil, val)
}
