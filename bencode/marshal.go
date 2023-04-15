package bencode

import (
	"errors"
	"io"
	"reflect"
	"strings"
)

// 将torrent格式的字符串转为go中的slice或struct，可以灵活处理不同类型的slice和struct
func Unmarshal(r io.Reader, s interface{}) error {
	o, err := Parse(r)
	if err != nil {
		return err
	}
	p := reflect.ValueOf(s)

	if p.Kind() != reflect.Ptr {
		return errors.New("dest must be a pointer")
	}

	// 通过Parse解析文本，看其是什么类型
	switch o.type_ {
	case BLIST:
		// 转成该格式
		// []*BObject
		list, _ := o.List()
		// 看传入的s是什么类型，创建一个和其类型相同的slice，长度为Parse解析出来的list的长度
		l := reflect.MakeSlice(p.Elem().Type(), len(list), len(list))
		// 将传入的s的指针指向新创建的slice，后续只要根据index设置相应值即可
		// 虽然传进来的是一个空的slice，但是在反射中做append是非常麻烦的，而且slice可能长度不为空或者与list的长度不匹配
		// 因此直接将其指针指向新分配的slice即可
		p.Elem().Set(l)
		err = unmarshalList(p, list)
		if err != nil {
			return err
		}
	case BDICT:
		// 如果是一个指向struct的指针，不用像slice那么麻烦，直接set相应的filed即可
		// 如果Parse解析出来的是Dict，根据Dict的key和value设置struct中相应的属性即可
		dict, _ := o.Dict()
		err = unmarshalDict(p, dict)
		if err != nil {
			return err
		}
	default:
		return errors.New("src code must be struct or slice")
	}
	return nil
}

// p.Kind must be Ptr && p.Elem().Type().Kind() must be Slice
func unmarshalList(p reflect.Value, list []*BObject) error {
	if p.Kind() != reflect.Ptr || p.Elem().Type().Kind() != reflect.Slice {
		return errors.New("dest must be pointer to slice")
	}

	// 长度就是新创建的slice的长度，根据index去填每个位置的数即可
	v := p.Elem()

	if len(list) == 0 {
		return nil
	}

	// 根据第一个元素的类型来确定list的类型，list中所有元素的类型都是相同的
	switch list[0].type_ {
	case BSTR:
		// 取出list中的每个元素，依次设置在v中相应的index中，此时p是一个slice
		for i, o := range list {
			val, err := o.Str()
			if err != nil {
				return err
			}
			v.Index(i).SetString(val)
		}
	case BINT:
		for i, o := range list {
			val, err := o.Int()
			if err != nil {
				return err
			}
			v.Index(i).SetInt(int64(val))
		}
	case BLIST:
		for i, o := range list {
			val, err := o.List()
			if err != nil {
				return err
			}

			if v.Type().Elem().Kind() != reflect.Slice {
				return ErrTyp
			}

			// 一个指针，指向slice
			lp := reflect.New(v.Type().Elem())
			// 创建一个新的slice，长度和val的长度一样，val为list内层的list
			ls := reflect.MakeSlice(v.Type().Elem(), len(val), len(val))

			lp.Elem().Set(ls)
			// 递归地调用unmarshalList，逐步解析
			err = unmarshalList(lp, val)
			if err != nil {
				return err
			}

			// 将v当前index的元素设置为指针lp指向的元素
			v.Index(i).Set(lp.Elem())
		}
	case BDICT:
		for i, o := range list {
			val, err := o.Dict()
			if err != nil {
				return err
			}

			if v.Type().Elem().Kind() != reflect.Struct {
				return ErrTyp
			}

			// 新建一个指针指向struct
			dp := reflect.New(v.Type().Elem())

			// 递归解析
			err = unmarshalDict(dp, val)

			if err != nil {
				return err
			}

			// 将v当前index的元素设置为指针dp指向的元素
			v.Index(i).Set(dp.Elem())
		}
	}
	return nil
}

// p.Kind() must be Ptr && p.Elem().Type().Kind() must be Struct
func unmarshalDict(p reflect.Value, dict map[string]*BObject) error {
	if p.Kind() != reflect.Ptr || p.Elem().Type().Kind() != reflect.Struct {
		return errors.New("dest must be pointer")
	}

	v := p.Elem()
	// 遍历v中所有的filed，设置每个filed的value
	// 这里遍历的是p指向的struct的每个filed，根据该filed从Dict中取值设置该filed
	for i, n := 0, v.NumField(); i < n; i++ {
		// value
		fv := v.Field(i)
		if !fv.CanSet() {
			continue
		}
		// filed
		ft := v.Type().Field(i)

		// 看有没有打上bencode的tag，如果有，就以该tag为key，否则用属性名的小写为key
		// 因为一般go中暴露出去的属性都是大写开头的，因此先将其转为小写
		key := ft.Tag.Get("bencode")
		if key == "" {
			key = strings.ToLower(ft.Name)
		}

		// 从转出来的dict中取出key对应的value
		fo := dict[key]
		if fo == nil {
			continue
		}

		// 根绝value的类型设置strcut中相应filed的值
		switch fo.type_ {
		case BSTR:
			// 每一个case都会判断是否可以set，若不能set则会直接报错
			if ft.Type.Kind() != reflect.String {
				break
			}
			val, _ := fo.Str()
			fv.SetString(val)
		case BINT:
			if ft.Type.Kind() != reflect.Int {
				break
			}
			val, _ := fo.Int()
			fv.SetInt(int64(val))
		case BLIST:
			if ft.Type.Kind() != reflect.Slice {
				break
			}

			// 下面的和list中一样
			list, _ := fo.List()
			lp := reflect.New(ft.Type)
			ls := reflect.MakeSlice(ft.Type, len(list), len(list))
			lp.Elem().Set(ls)
			err := unmarshalList(lp, list)
			if err != nil {
				break
			}
			// 给当前filed设定指针lp的值
			fv.Set(lp.Elem())
		case BDICT:
			if ft.Type.Kind() != reflect.Struct {
				break
			}

			// 下面的和list中一样
			dp := reflect.New(ft.Type)
			dict, _ := fo.Dict()
			err := unmarshalDict(dp, dict)
			if err != nil {
				break
			}
			// 给当前filed设定指针dp的值
			fv.Set(dp.Elem())
		}
	}
	return nil
}

func marshalValue(w io.Writer, v reflect.Value) int {
	len := 0
	switch v.Kind() {
	case reflect.String:
		len += EncodeString(w, v.String())
	case reflect.Int:
		len += EncodeInt(w, int(v.Int()))
	case reflect.Slice:
		len += marshalList(w, v)
	case reflect.Struct:
		len += marshalDict(w, v)
	}
	return len
}

func marshalList(w io.Writer, vl reflect.Value) int {
	// 为开头的l和结尾的e，共两个字符
	len := 2
	w.Write([]byte{'l'})

	// 遍历list中的元素
	for i := 0; i < vl.Len(); i++ {
		ev := vl.Index(i)
		len += marshalValue(w, ev)
	}

	w.Write([]byte{'e'})
	return len
}

func marshalDict(w io.Writer, vd reflect.Value) int {
	// 为开头的d和结尾的e，共两个字符
	len := 2
	w.Write([]byte{'d'})

	// 遍历map中的元素
	for i := 0; i < vd.NumField(); i++ {
		fv := vd.Field(i)
		ft := vd.Type().Field(i)
		key := ft.Tag.Get("bencode")
		if key == "" {
			key = strings.ToLower(ft.Name)
		}
		len += EncodeString(w, key)
		len += marshalValue(w, fv)
	}

	w.Write([]byte{'e'})
	return len
}

// 将go中的slice或struct转为torrent格式的字符串，可以灵活处理不同类型的slice和struct
func Marshal(w io.Writer, s interface{}) int {
	v := reflect.ValueOf(s)
	// 这里只需要指针指向位置的值，将其转为字符串，因此使用该操作使其兼容指针
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	return marshalValue(w, v)
}
