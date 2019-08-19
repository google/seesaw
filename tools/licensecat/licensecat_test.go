package main

import (
	"reflect"
	"testing"
)

func TestGetPackageNames(t *testing.T) {
	data := []byte(`# github.com/appscode/jsonpatch v0.0.0-20190108182946-7c0e3b262f30
github.com/appscode/jsonpatch
# github.com/beorn7/perks v0.0.0-20180321164747-3a771d992973
github.com/beorn7/perks/quantile
# k8s.io/api v0.0.0-20190627205229-acea843d18eb => k8s.io/api v0.0.0-20181213150558-05914d821849
k8s.io/api/apps/v1beta2
k8s.io/api/core/v1
k8s.io/api/policy/v1beta1
`)
	wantNames := []string{
		"github.com/appscode/jsonpatch",
		"github.com/beorn7/perks",
		"k8s.io/api",
	}

	gotNames, err := getPackageNames(data)
	if err != nil {
		t.Errorf("getPackage() = err %v, want %v", err, wantNames)
	}
	if !reflect.DeepEqual(gotNames, wantNames) {
		t.Errorf("getPackage() = %v, want %v", gotNames, wantNames)
	}
}

func TestFmtLicense(t *testing.T) {
	fname := "github.com/foo/bar/LICENSE"
	text := `The MIT License (MIT)

Copyright (c) 2019 Foo Bar

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`
	wantResult := `# github.com/foo/bar/LICENSE

The MIT License (MIT)

Copyright (c) 2019 Foo Bar

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`
	gotResult := fmtLicense(fname, text)
	if gotResult != wantResult {
		t.Errorf("fmtLicense() = %v, want %v", gotResult, wantResult)
	}
}
