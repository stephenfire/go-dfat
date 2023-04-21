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

func (p parser0) ForAssign0(indexOfParent int, name string, property int) error {
	fmt.Printf("ForAssign0(Index:%d name:%s prop:%d)\n", indexOfParent, name, property)
	return nil
}

func (p parser0) ForContainerPtr(indexOfParent, size int, startOrEnd bool, name string, property interface{}) (goin bool, err error) {
	fmt.Printf("ForKindPtr(index:%d size:%d start:%t name:%s property:%s)\n",
		indexOfParent, size, startOrEnd, name, reflect.TypeOf(property))
	return true, nil
}

func (p parser0) ForContainerStruct(indexOfParent, size int, startOrEnd bool, name string, property interface{}) (goin bool, err error) {
	fmt.Printf("ForContainerStruct(index:%d size:%d start:%t name:%s property:%s)\n",
		indexOfParent, size, startOrEnd, name, reflect.TypeOf(property))
	return true, nil
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
	p := parser0{}
	tr, err := NewTraveller(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = tr.Traverse(i); err != nil {
		t.Fatal(err)
	}
}
