package health

import (
	"reflect"
	"testing"
)

func TestChunk(t *testing.T) {
	cases := []struct {
		in   []int
		n    int
		want [][]int
	}{
		{[]int{1, 2, 3, 4, 5}, 2, [][]int{{1, 2}, {3, 4}, {5}}},
		{[]int{1, 2, 3, 4}, 2, [][]int{{1, 2}, {3, 4}}},
		{[]int{1, 2, 3}, 10, [][]int{{1, 2, 3}}},
		{[]int{}, 3, nil},
		{[]int{1, 2}, 0, [][]int{{1}, {2}}}, // n<=0 は 1 として扱う
	}
	for _, tc := range cases {
		if got := chunk(tc.in, tc.n); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("chunk(%v,%d) = %v, want %v", tc.in, tc.n, got, tc.want)
		}
	}
}
