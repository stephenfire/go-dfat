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
)

var (
	ErrInvalidAdapter = errors.New("invalid adapter")
	ErrWant2Returns   = errors.New("expecting returns (goin bool, err error)")
	ErrWant1Return    = errors.New("expecting returns (err error)")

	_kindMap = map[string]reflect.Kind{
		"Bool":          reflect.Bool,
		"Int":           reflect.Int,
		"Int8":          reflect.Int8,
		"Int16":         reflect.Int16,
		"Int64":         reflect.Int64,
		"Uint":          reflect.Uint,
		"Uint8":         reflect.Uint8,
		"Uint16":        reflect.Uint16,
		"Uint32":        reflect.Uint32,
		"Uint64":        reflect.Uint64,
		"Uintptr":       reflect.Uintptr,
		"Float32":       reflect.Float32,
		"Float64":       reflect.Float64,
		"Complex64":     reflect.Complex64,
		"Complex128":    reflect.Complex128,
		"Array":         reflect.Array,
		"Chan":          reflect.Chan,
		"Func":          reflect.Func,
		"Interface":     reflect.Interface,
		"Map":           reflect.Map,
		"Ptr":           reflect.Ptr,
		"Slice":         reflect.Slice,
		"String":        reflect.String,
		"Struct":        reflect.Struct,
		"UnsafePointer": reflect.UnsafePointer,
		"Pointer":       reflect.Ptr,
	}

	_containers = map[reflect.Kind]struct{}{
		reflect.Array:  {},
		reflect.Map:    {},
		reflect.Ptr:    {},
		reflect.Slice:  {},
		reflect.Struct: {},
	}

	_typeOfString = reflect.TypeOf((*string)(nil)).Elem()
	_typeOfBool   = reflect.TypeOf(true)
	_typeOfInt    = reflect.TypeOf(int(0))
	_typeOfError  = reflect.TypeOf((*error)(nil)).Elem()
)

const (
	ForImpl      ItemType = 0
	ForAssign    ItemType = 1
	ForKind      ItemType = 2
	ForContainer ItemType = 3
	Unknown      ItemType = 0xff

	ImplPrefix       = "ForImpl"
	AssignPrefix     = "ForAssign"
	KindPrefix       = "ForKind"
	ContainerPrefix  = "ForContainer"
	_minPrefixLength = 7
)

// Traveller 将一个对象中所有公开属性进行依次深度优先遍历，即当对象中包含另一个对象时，则先对子对象的公开属
// 性进行遍历，直到该子对象遍历完后，才对该子对象后续兄弟对象进行遍历。
// adapter实现多个方法，每个方法用来接收一个对象正在被遍历的公开属性，用来对其进行处理。如果遍历的某个属性没有对应方法则忽略并继续。
// 方法分为2种，
// 针对interface的方法：由方法名前缀、方法名及Tag标识确定绑定关系, ForImplxxxxx
// 针对struct的方法：由方法名前缀、方法名及参数类型确定绑定关系, Tag表明序号，没有tag时则为声明序, ForAssignxxxxx
// 针对Kind的方法：由方法名前缀、方法名及Tag标识确定绑定关系, ForKindxxxxx
// 首先确定当前属性是否实现绑定的interface
// 再根据遍历中当前属性的类型找到对应方法:
//		与方法声明类型完全一致时，则直接使用
//		如果属性类型可以AssignableTo，则找出其中序号最靠前的 ()
// 如果没有，则用类型对应的Kind找方法。
// 如果都没有，则忽略
type (
	ItemType uint8

	orderItem struct {
		i int          // index of the method list of adapter
		n string       // name of the method
		o int          // order, not in use
		t reflect.Type // type of property bound by the method
		c bool         // if the property is a container
		k reflect.Kind // kind of property bound by the method, only one of t!=nil or k!=0
	}

	orderItems []orderItem

	Property struct {
		Index        int    // index for reflect.Value.Field(), if -1,placeholder, return zero value, no corresponding property in the struct
		Name         string // field name
		IndexForReal int    // index for Traveller, -1: use Index instead
	}

	StructPropertier interface {
		Properties(structVal reflect.Value) (size int, avails []Property) // sorted by (IndexForReal, Index)
	}

	TraverseConf struct {
		// if false (by default), error would occured if there's no binding function found for a Property
		IgnoreMissedBinding bool
		// user defined struct property parser, if nil, use default implements in the package
		Propertier StructPropertier
		// whether to call the end method after the container ends
		ContainerEnd bool
	}

	parentInfo struct {
		value        reflect.Value // container value
		size         int           // container size: Array/Slice.Len(), len(Map.MapKeys())*2, len([]Property)
		offset       int           // current calling child value index [0, size)
		structFields []Property    // properties if value is a struct
		binding      reflect.Value // container binding start/end function
	}
)

func (ItemType) Which(name string) (ItemType, reflect.Kind, bool) {
	if len(name) < _minPrefixLength {
		return Unknown, reflect.Invalid, false
	}
	if name[:len(ImplPrefix)] == ImplPrefix {
		return ForImpl, reflect.Invalid, true
	} else if len(name) >= len(AssignPrefix) && name[:len(AssignPrefix)] == AssignPrefix {
		return ForAssign, reflect.Invalid, true
	} else if name[:len(KindPrefix)] == KindPrefix {
		suffix := name[len(KindPrefix):]
		kind, ok := _kindMap[suffix]
		if !ok {
			return Unknown, reflect.Invalid, false
		}
		if _, ok = _containers[kind]; ok {
			return Unknown, reflect.Invalid, false
		}
		return ForKind, kind, true
	} else if name[:len(ContainerPrefix)] == ContainerPrefix {
		suffix := name[len(ContainerPrefix):]
		kind, ok := _kindMap[suffix]
		if !ok {
			return Unknown, reflect.Invalid, false
		}
		if _, ok = _containers[kind]; !ok {
			return Unknown, reflect.Invalid, false
		}
		return ForContainer, kind, true
	} else {
		return Unknown, reflect.Invalid, false
	}
}

// IsValidWithReceiver
// binding function signatures:
// ForImplxxxx(IndexInParent, PropertyName, Property) error
// ForAssignxxxx(IndexInParent, PropertyName, Property) error
// ForKind:
//   normal kinds: ForKindYYYY(IndexInParent, PropertyName, Property) error,
//   	YYYY must be a key in _kindMap, and the Kind must not be a container.
//   container kinds:
//   	ForContainerYYYY(IndexInParent, Size, StartOrEnd, PropertyName, Property) (goin bool, err error),
//   	YYYY must be a key in _containers
func (i ItemType) IsValidWithReceiver(method reflect.Method) bool {
	if !method.Func.IsValid() {
		return false
	}
	ftype := method.Func.Type()
	paramSize := ftype.NumIn()
	switch i {
	case ForImpl, ForAssign, ForKind:
		if paramSize != 4 {
			return false
		}
		if ftype.In(1) != _typeOfInt || ftype.In(2) != _typeOfString {
			return false
		}
		if ftype.NumOut() != 1 || ftype.Out(0) != _typeOfError {
			return false
		}
		return true
	case ForContainer:
		if paramSize != 6 {
			return false
		}
		if ftype.In(1) != _typeOfInt || ftype.In(2) != _typeOfInt ||
			ftype.In(3) != _typeOfBool || ftype.In(4) != _typeOfString {
			return false
		}
		if ftype.NumOut() != 2 || ftype.Out(0) != _typeOfBool || ftype.Out(1) != _typeOfError {
			return false
		}
		return true
	default:
		return false
	}
}

func (i ItemType) String() string {
	switch i {
	case ForImpl:
		return ImplPrefix
	case ForAssign:
		return AssignPrefix
	case ForKind:
		return KindPrefix
	case ForContainer:
		return ContainerPrefix
	case Unknown:
		return "Unknown"
	default:
		return "N/A"
	}
}

func (i orderItem) Type() (ItemType, bool) {
	if i.t != nil {
		if k := i.t.Kind(); k == reflect.Interface {
			return ForImpl, true
		} else {
			return ForAssign, true
		}
	} else if _, exist := _containers[i.k]; exist {
		return ForContainer, true
	} else if i.k != reflect.Invalid {
		return ForKind, true
	}
	return Unknown, false
}

func (i orderItem) match(typ reflect.Type) (reflect.Type, reflect.Kind, bool) {
	if typ == nil {
		return nil, reflect.Invalid, false
	}
	if i.t != nil {
		if i.t.Kind() == reflect.Interface {
			if typ.Implements(i.t) {
				return i.t, reflect.Invalid, true
			} else {
				return nil, reflect.Invalid, false
			}
		} else {
			if typ.AssignableTo(i.t) {
				return i.t, reflect.Invalid, true
			} else {
				return nil, reflect.Invalid, false
			}
		}
	} else {
		if typ.Kind() == i.k {
			return nil, i.k, true
		} else {
			return nil, reflect.Invalid, false
		}
	}
}

func (i orderItem) parseReturns(outs []reflect.Value) (goin bool, err error) {
	if i.c {
		if len(outs) != 2 {
			return false, ErrWant2Returns
		}
		if outs[0].Kind() != reflect.Bool || !outs[1].Type().Implements(_typeOfError) {
			return false, ErrWant2Returns
		}
		if !outs[1].IsZero() {
			err = outs[1].Interface().(error)
		}
		return outs[0].Bool(), err
	} else {
		if len(outs) != 1 {
			return false, ErrWant1Return
		}
		if !outs[0].Type().Implements(_typeOfError) {
			return false, ErrWant1Return
		}
		if !outs[0].IsZero() {
			err = outs[0].Interface().(error)
		}
		return false, err
	}
}

func (i orderItem) String() string {
	typ, _ := i.Type()
	str := fmt.Sprintf("Idx:%d Order:%d Name:%s", i.i, i.o, i.n)
	container := ""
	if i.c {
		container = " Container"
	}
	switch typ {
	case ForImpl:
		return fmt.Sprintf("Item{%s Impl:%s%s}", str, i.t.String(), container)
	case ForAssign:
		return fmt.Sprintf("Item{%s Assign:%s%s}", str, i.t.String(), container)
	case ForKind:
		return fmt.Sprintf("Item{%s Kind:%s%s}", str, i.k.String(), container)
	default:
		return fmt.Sprintf("Item{%s %s%s}", str, typ, container)
	}
}

func (is orderItems) Len() int {
	return len(is)
}

func (is orderItems) Swap(i, j int) {
	is[i], is[j] = is[j], is[i]
}

func (is orderItems) Less(i, j int) bool {
	if is[i].o < is[j].o {
		return true
	} else if is[i].o == is[j].o {
		return is[i].i < is[j].i
	} else {
		return false
	}
}

func (p Property) String() string {
	if p.IndexForReal >= 0 {
		return fmt.Sprintf("{%d(%d).%s}", p.Index, p.IndexForReal, p.Name)
	}
	return fmt.Sprintf("{%d.%s}", p.Index, p.Name)
}

func (c *TraverseConf) String() string {
	if c == nil {
		return "Conf<nil>"
	}
	propertier := ""
	if c.Propertier != nil {
		propertier = " hasPropertier"
	}
	return fmt.Sprintf("Conf{IgnoreMissedBinding:%t%s}", c.IgnoreMissedBinding, propertier)
}

func (c *TraverseConf) Clone() *TraverseConf {
	if c == nil {
		return nil
	}
	return &TraverseConf{
		IgnoreMissedBinding: c.IgnoreMissedBinding,
		Propertier:          c.Propertier,
	}
}

func (p *parentInfo) String() string {
	if p == nil {
		return "<nil>"
	}
	if !p.value.IsValid() {
		return "{}"
	}
	if p.value.Type().Kind() == reflect.Struct {
		return fmt.Sprintf("{%s size:%d offset:%d fields:%s binding:%t}",
			p.value.Type().Name(), p.size, p.offset, p.structFields, p.binding.IsValid())
	}
	return fmt.Sprintf("{%s size:%d offset:%d binding:%t}",
		p.value.Type().Name(), p.size, p.offset, p.binding.IsValid())
}

func (p *parentInfo) isValid() bool {
	return p != nil && p.value.IsValid()
}

func (p *parentInfo) callIns(val reflect.Value) []reflect.Value {
	ret := make([]reflect.Value, 3)
	if p != nil && p.value.IsValid() {
		if len(p.structFields) > 0 && p.offset >= 0 && p.offset < len(p.structFields) {
			if p.structFields[p.offset].IndexForReal >= 0 {
				ret[0] = reflect.ValueOf(p.structFields[p.offset].IndexForReal)
			} else {
				ret[0] = reflect.ValueOf(p.structFields[p.offset].Index)
			}
			ret[1] = reflect.ValueOf(p.structFields[p.offset].Name)
		} else {
			ret[0] = reflect.ValueOf(p.offset)
			ret[1] = reflect.ValueOf("")
		}
	} else {
		ret[0] = reflect.ValueOf(int(-1))
		ret[1] = reflect.ValueOf("")
	}
	ret[2] = val
	return ret
}

func (p *parentInfo) _containerIns(info *parentInfo, startOrEnd bool, val reflect.Value) []reflect.Value {
	ret := make([]reflect.Value, 5)
	if p != nil && p.value.IsValid() {
		ret[0] = reflect.ValueOf(p.offset)
		if len(p.structFields) > 0 && p.offset >= 0 && p.offset < len(p.structFields) {
			ret[3] = reflect.ValueOf(p.structFields[p.offset].Name)
		} else {
			ret[3] = reflect.ValueOf("")
		}
	} else {
		ret[0] = reflect.ValueOf(int(-1))
		ret[3] = reflect.ValueOf("")
	}
	ret[1] = reflect.ValueOf(info.size)
	ret[2] = reflect.ValueOf(startOrEnd)
	ret[4] = val
	return ret
}

func (p *parentInfo) startContainerIns(info *parentInfo, val reflect.Value) []reflect.Value {
	return p._containerIns(info, true, val)
}

func (p *parentInfo) endContainerIns(info *parentInfo, val reflect.Value) []reflect.Value {
	return p._containerIns(info, false, val)
}
