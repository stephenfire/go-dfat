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
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type Inner0 struct {
	A int `rtlorder:"0"`
	E int `rtlorder:"5"`
	B int `rtlorder:"1"`
	C int `rtlorder:"3"`
	D int `rtlorder:"4"`
	x int
	y int
}

type parser0 struct{}

func (p parser0) ForAssign0(depth, indexOfParent int, name string, property int) error {
	fmt.Printf("ForAssign0(Depth:%d Index:%d name:%s prop:%d)\n", depth, indexOfParent, name, property)
	return nil
}

func (p parser0) ForContainerStruct(depth, indexOfParent, size int, startOrEnd bool, name string, property interface{}) (goin bool, err error) {
	fmt.Printf("ForContainerStruct(depth:%d index:%d size:%d start:%t name:%s property:%s)\n",
		depth, indexOfParent, size, startOrEnd, name, reflect.TypeOf(property))
	return true, nil
}

type parser1 struct {
	parser0
}

func (p parser1) ForContainerPtr(depth, indexOfParent, size int, startOrEnd bool, name string, property interface{}) (goin bool, err error) {
	fmt.Printf("ForKindPtr(depth:%d index:%d size:%d start:%t name:%s property:%s)\n",
		depth, indexOfParent, size, startOrEnd, name, reflect.TypeOf(property))
	return true, nil
}

type rtlpropertier struct{}

func (p rtlpropertier) Properties(val reflect.Value) (size int, fields []Property) {
	if !val.IsValid() || val.Type().Kind() != reflect.Struct {
		return 0, nil
	}
	typ := val.Type()
	for i := 0; i < typ.NumField(); i++ {
		// exported field
		if f := typ.Field(i); f.PkgPath == "" {
			tagStr := f.Tag.Get("rtl")
			ignored := false
			for _, tag := range strings.Split(tagStr, ",") {
				switch tag = strings.TrimSpace(tag); tag {
				case "-":
					ignored = true
				}
			}
			if ignored {
				continue
			}

			order := -1
			tagStr = f.Tag.Get("rtlorder")
			tagStr = strings.TrimSpace(tagStr)
			if len(tagStr) > 0 {
				if oi, err := strconv.Atoi(tagStr); err != nil {
					panic(fmt.Errorf("illegal rtlorder (%s) for field %s of type %s",
						tagStr, f.Name, typ.Name()))
				} else {
					if oi < 0 {
						panic(fmt.Errorf("illegal rtlorder (%s) for field %s of type %s",
							tagStr, f.Name, typ.Name()))
					}
					order = oi
				}
			}

			fields = append(fields, Property{i, f.Name, order})
		}
	}
	sort.SliceStable(fields, func(i, j int) bool {
		if fields[i].IndexForReal > fields[j].IndexForReal {
			return false
		}
		if fields[i].IndexForReal < fields[j].IndexForReal {
			return true
		}
		return fields[i].Index < fields[j].Index
	})
	for i := 0; i < len(fields); i++ {
		if fields[i].IndexForReal < 0 {
			fields[i].IndexForReal = i
		} else {
			if fields[i].IndexForReal < i {
				panic(fmt.Errorf("illegal rtlorder (%d) for field %s of type %s, should >= %d",
					fields[i].IndexForReal, fields[i].Name, typ.Name(), i))
			}
		}
	}
	size = 0
	if len(fields) > 0 {
		size = fields[len(fields)-1].IndexForReal + 1
	}
	return
}

func TestStruct(t *testing.T) {
	i := &Inner0{
		A: 1,
		E: 2,
		B: 3,
		C: 4,
		D: 5,
		x: 6,
		y: 7,
	}
	var i1 *Inner0

	{
		t.Log("parser0")
		p := parser0{}
		tr, err := NewTraveller(p, &TraverseConf{PtrAutoGoIn: true})
		if err != nil {
			t.Fatal(err)
		}
		if err = tr.Traverse(i); err != nil {
			t.Fatal(err)
		}

		t.Log("nil")
		if err = tr.Traverse(i1); err != nil {
			t.Fatal(err)
		}
	}
	{
		t.Log("parser1")
		p := parser1{}
		tr, err := NewTraveller(p, &TraverseConf{PtrAutoGoIn: true})
		if err != nil {
			t.Fatal(err)
		}
		if err = tr.Traverse(i); err != nil {
			t.Fatal(err)
		}

		t.Log("nil")
		if err = tr.Traverse(i1); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWithSelfParser(t *testing.T) {
	i := &Inner0{
		A: 1,
		E: 2,
		B: 3,
		C: 4,
		D: 5,
		x: 6,
		y: 7,
	}
	p := parser0{}
	tr, err := NewTraveller(p, &TraverseConf{Propertier: rtlpropertier{}, PtrAutoGoIn: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = tr.Traverse(i); err != nil {
		t.Fatal(err)
	}
}
