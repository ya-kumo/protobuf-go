// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package prototext

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/internal/encoding/messageset"
	"google.golang.org/protobuf/internal/encoding/text"
	"google.golang.org/protobuf/internal/errors"
	"google.golang.org/protobuf/internal/flags"
	"google.golang.org/protobuf/internal/genid"
	"google.golang.org/protobuf/internal/order"
	"google.golang.org/protobuf/internal/pragma"
	"google.golang.org/protobuf/internal/strs"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const defaultIndent = "  "

// Format formats the message as a multiline string.
// This function is only intended for human consumption and ignores errors.
// Do not depend on the output being stable. Its output will change across
// different builds of your program, even when using the same version of the
// protobuf module.
func Format(m proto.Message) string {
	return MarshalOptions{Multiline: true}.Format(m)
}

// Marshal writes the given [proto.Message] in textproto format using default
// options. Do not depend on the output being stable. Its output will change
// across different builds of your program, even when using the same version of
// the protobuf module.
func Marshal(m proto.Message) ([]byte, error) {
	return MarshalOptions{}.Marshal(m)
}

// MarshalOptions is a configurable text format marshaler.
type MarshalOptions struct {
	pragma.NoUnkeyedLiterals

	// Multiline specifies whether the marshaler should format the output in
	// indented-form with every textual element on a new line.
	// If Indent is an empty string, then an arbitrary indent is chosen.
	Multiline bool

	// Indent specifies the set of indentation characters to use in a multiline
	// formatted output such that every entry is preceded by Indent and
	// terminated by a newline. If non-empty, then Multiline is treated as true.
	// Indent can only be composed of space or tab characters.
	Indent string

	// EmitASCII specifies whether to format strings and bytes as ASCII only
	// as opposed to using UTF-8 encoding when possible.
	EmitASCII bool

	// allowInvalidUTF8 specifies whether to permit the encoding of strings
	// with invalid UTF-8. This is unexported as it is intended to only
	// be specified by the Format method.
	allowInvalidUTF8 bool

	// AllowPartial allows messages that have missing required fields to marshal
	// without returning an error. If AllowPartial is false (the default),
	// Marshal will return error if there are any missing required fields.
	AllowPartial bool

	// EmitUnknown specifies whether to emit unknown fields in the output.
	// If specified, the unmarshaler may be unable to parse the output.
	// The default is to exclude unknown fields.
	EmitUnknown bool

	// Resolver is used for looking up types when expanding google.protobuf.Any
	// messages. If nil, this defaults to using protoregistry.GlobalTypes.
	Resolver interface {
		protoregistry.ExtensionTypeResolver
		protoregistry.MessageTypeResolver
	}

	CustomedEncoder EncoderDelegate
	Override        EncoderOverride
}

// Format formats the message as a string.
// This method is only intended for human consumption and ignores errors.
// Do not depend on the output being stable. Its output will change across
// different builds of your program, even when using the same version of the
// protobuf module.
func (o MarshalOptions) Format(m proto.Message) string {
	if m == nil || !m.ProtoReflect().IsValid() {
		return "<nil>" // invalid syntax, but okay since this is for debugging
	}
	o.allowInvalidUTF8 = true
	o.AllowPartial = true
	o.EmitUnknown = true
	b, _ := o.Marshal(m)
	return string(b)
}

// Marshal writes the given [proto.Message] in textproto format using options in
// MarshalOptions object. Do not depend on the output being stable. Its output
// will change across different builds of your program, even when using the
// same version of the protobuf module.
func (o MarshalOptions) Marshal(m proto.Message) ([]byte, error) {
	return o.marshal(nil, m)
}

// MarshalAppend appends the textproto format encoding of m to b,
// returning the result.
func (o MarshalOptions) MarshalAppend(b []byte, m proto.Message) ([]byte, error) {
	return o.marshal(b, m)
}

// marshal is a centralized function that all marshal operations go through.
// For profiling purposes, avoid changing the name of this function or
// introducing other code paths for marshal that do not go through this.
func (o MarshalOptions) marshal(b []byte, m proto.Message) ([]byte, error) {
	delims := [2]byte{'{', '}'}

	if o.Multiline && o.Indent == "" {
		o.Indent = defaultIndent
	}
	if o.Resolver == nil {
		o.Resolver = protoregistry.GlobalTypes
	}

	internalEnc, err := text.NewEncoder(b, o.Indent, delims, o.EmitASCII)
	if err != nil {
		return nil, err
	}

	// Treat nil message interface as an empty message,
	// in which case there is nothing to output.
	if m == nil {
		return b, nil
	}

	baseEnc := &Encoder{
		Encoder:  internalEnc,
		opts:     o,
		delegate: nil,
		override: EncoderOverride{},
	}
	enc := o.CustomedEncoder
	if enc == nil {
		enc = baseEnc
	} else {
		baseEnc.delegate = o.CustomedEncoder
		baseEnc.override = o.Override
		enc.SetBase(baseEnc)
	}
	err = enc.MarshalMessage(m.ProtoReflect(), false)
	if err != nil {
		return nil, err
	}
	out := enc.Bytes()
	if len(o.Indent) > 0 && len(out) > 0 {
		out = append(out, '\n')
	}
	if o.AllowPartial {
		return out, nil
	}
	return out, proto.CheckInitialized(m)
}

// EncoderOverride defines which overrode methods should be used.
type EncoderOverride struct {
	// HasMarshalMessage indicates whether [EncoderDelegate.MarshalMessage] should be used.
	HasMarshalMessage bool

	// HasMarshalField indicates whether [EncoderDelegate.MarshalField] should be used.
	HasMarshalField bool

	// HasMarshalSingular indicates whether [EncoderDelegate.MarshalSingular] should be used.
	HasMarshalSingular bool

	// HasMarshalList indicates whether [EncoderDelegate.MarshalList] should be used.
	HasMarshalList bool

	// HasMarshalMap indicates whether [EncoderDelegate.MarshalMap] should be used.
	HasMarshalMap bool

	// HasMarshalUnknown indicates whether [EncoderDelegate.MarshalUnknown] should be used.
	HasMarshalUnknown bool

	// HasMarshalAny indicates whether [EncoderDelegate.MarshalAny] should be used.
	HasMarshalAny bool
}

// EncoderDelegate defines the overrideable methods of [Encoder].
// Besides implementing override methods, a struct [EncoderOverride] is also required
// to make it usable.
type EncoderDelegate interface {
	// SetBase will be called with the default [Encoder], makes it easier to inherit
	// default behaviors.
	SetBase(*Encoder)

	// MarshalMessage can override [Encoder.MarshalMessage].
	MarshalMessage(m protoreflect.Message, inclDelims bool) error

	// MarshalField can override [Encoder.MarshalField].
	MarshalField(name string, val protoreflect.Value, fd protoreflect.FieldDescriptor) error

	// MarshalField can override [Encoder.MarshalSingular].
	MarshalSingular(val protoreflect.Value, fd protoreflect.FieldDescriptor) error

	// MarshalField can override [Encoder.MarshalList].
	MarshalList(name string, list protoreflect.List, fd protoreflect.FieldDescriptor) error

	// MarshalField can override [Encoder.MarshalMap].
	MarshalMap(name string, mmap protoreflect.Map, fd protoreflect.FieldDescriptor) error

	// MarshalField can override [Encoder.MarshalUnknown].
	MarshalUnknown(b []byte)

	// MarshalField can override [Encoder.MarshalAny].
	MarshalAny(m protoreflect.Message) bool

	// Bytes is called by [MarshalOptions] and should not be overridden if [Encoder] is embedded.
	Bytes() []byte
}

// Encoder became public for customization.
type Encoder struct {
	*text.Encoder

	opts     MarshalOptions
	delegate EncoderDelegate
	override EncoderOverride
}

func (e *Encoder) SetBase(_ *Encoder) {
	panic("should not set base in base, consider define SetBase method in your struct")
}

// MarshalMessage marshals the given protoreflect.Message.
func (e *Encoder) MarshalMessage(m protoreflect.Message, inclDelims bool) error {
	messageDesc := m.Descriptor()
	if !flags.ProtoLegacy && messageset.IsMessageSet(messageDesc) {
		return errors.New("no support for proto1 MessageSets")
	}

	if inclDelims {
		e.StartMessage()
		defer e.EndMessage()
	}

	// Handle Any expansion.
	if messageDesc.FullName() == genid.Any_message_fullname {
		if e.marshalAny(m) {
			return nil
		}
		// If unable to expand, continue on to marshal Any as a regular message.
	}

	// Marshal fields.
	var err error
	order.RangeFields(m, order.IndexNameFieldOrder, func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if err = e.marshalField(fd.TextName(), v, fd); err != nil {
			return false
		}
		return true
	})
	if err != nil {
		return err
	}

	// Marshal unknown fields.
	if e.opts.EmitUnknown {
		e.marshalUnknown(m.GetUnknown())
	}

	return nil
}

// MarshalField marshals the given field with protoreflect.Value.
func (e *Encoder) MarshalField(name string, val protoreflect.Value, fd protoreflect.FieldDescriptor) error {
	switch {
	case fd.IsList():
		return e.marshalList(name, val.List(), fd)
	case fd.IsMap():
		return e.marshalMap(name, val.Map(), fd)
	default:
		e.WriteName(name)
		return e.marshalSingular(val, fd)
	}
}

// MarshalSingular marshals the given non-repeated field value. This includes
// all scalar types, enums, messages, and groups.
func (e *Encoder) MarshalSingular(val protoreflect.Value, fd protoreflect.FieldDescriptor) error {
	kind := fd.Kind()
	switch kind {
	case protoreflect.BoolKind:
		e.WriteBool(val.Bool())

	case protoreflect.StringKind:
		s := val.String()
		if !e.opts.allowInvalidUTF8 && strs.EnforceUTF8(fd) && !utf8.ValidString(s) {
			return errors.InvalidUTF8(string(fd.FullName()))
		}
		e.WriteString(s)

	case protoreflect.Int32Kind, protoreflect.Int64Kind,
		protoreflect.Sint32Kind, protoreflect.Sint64Kind,
		protoreflect.Sfixed32Kind, protoreflect.Sfixed64Kind:
		e.WriteInt(val.Int())

	case protoreflect.Uint32Kind, protoreflect.Uint64Kind,
		protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
		e.WriteUint(val.Uint())

	case protoreflect.FloatKind:
		// Encoder.WriteFloat handles the special numbers NaN and infinites.
		e.WriteFloat(val.Float(), 32)

	case protoreflect.DoubleKind:
		// Encoder.WriteFloat handles the special numbers NaN and infinites.
		e.WriteFloat(val.Float(), 64)

	case protoreflect.BytesKind:
		e.WriteString(string(val.Bytes()))

	case protoreflect.EnumKind:
		num := val.Enum()
		if desc := fd.Enum().Values().ByNumber(num); desc != nil {
			e.WriteLiteral(string(desc.Name()))
		} else {
			// Use numeric value if there is no enum description.
			e.WriteInt(int64(num))
		}

	case protoreflect.MessageKind, protoreflect.GroupKind:
		return e.marshalMessage(val.Message(), true)

	default:
		panic(fmt.Sprintf("%v has unknown kind: %v", fd.FullName(), kind))
	}
	return nil
}

// MarshalList marshals the given protoreflect.List as multiple name-value fields.
func (e *Encoder) MarshalList(name string, list protoreflect.List, fd protoreflect.FieldDescriptor) error {
	size := list.Len()
	for i := range size {
		e.WriteName(name)
		if err := e.marshalSingular(list.Get(i), fd); err != nil {
			return err
		}
	}
	return nil
}

// MarshalMap marshals the given protoreflect.Map as multiple name-value fields.
func (e *Encoder) MarshalMap(name string, mmap protoreflect.Map, fd protoreflect.FieldDescriptor) error {
	var err error
	order.RangeEntries(mmap, order.GenericKeyOrder, func(key protoreflect.MapKey, val protoreflect.Value) bool {
		e.WriteName(name)
		e.StartMessage()
		defer e.EndMessage()

		e.WriteName(string(genid.MapEntry_Key_field_name))
		err = e.marshalSingular(key.Value(), fd.MapKey())
		if err != nil {
			return false
		}

		e.WriteName(string(genid.MapEntry_Value_field_name))
		err = e.marshalSingular(val, fd.MapValue())
		return err == nil
	})
	return err
}

// MarshalUnknown parses the given []byte and marshals fields out.
// This function assumes proper encoding in the given []byte.
func (e *Encoder) MarshalUnknown(b []byte) {
	const dec = 10
	const hex = 16
	for len(b) > 0 {
		num, wtype, n := protowire.ConsumeTag(b)
		b = b[n:]
		e.WriteName(strconv.FormatInt(int64(num), dec))

		switch wtype {
		case protowire.VarintType:
			var v uint64
			v, n = protowire.ConsumeVarint(b)
			e.WriteUint(v)
		case protowire.Fixed32Type:
			var v uint32
			v, n = protowire.ConsumeFixed32(b)
			e.WriteLiteral("0x" + strconv.FormatUint(uint64(v), hex))
		case protowire.Fixed64Type:
			var v uint64
			v, n = protowire.ConsumeFixed64(b)
			e.WriteLiteral("0x" + strconv.FormatUint(v, hex))
		case protowire.BytesType:
			var v []byte
			v, n = protowire.ConsumeBytes(b)
			e.WriteString(string(v))
		case protowire.StartGroupType:
			e.StartMessage()
			var v []byte
			v, n = protowire.ConsumeGroup(num, b)
			e.marshalUnknown(v)
			e.EndMessage()
		default:
			panic(fmt.Sprintf("prototext: error parsing unknown field wire type: %v", wtype))
		}

		b = b[n:]
	}
}

// MarshalAny marshals the given google.protobuf.Any message in expanded form.
// It returns true if it was able to marshal, else false.
func (e *Encoder) MarshalAny(msg protoreflect.Message) bool {
	// Construct the embedded message.
	fds := msg.Descriptor().Fields()
	fdType := fds.ByNumber(genid.Any_TypeUrl_field_number)
	typeURL := msg.Get(fdType).String()
	mt, err := e.opts.Resolver.FindMessageByURL(typeURL)
	if err != nil {
		return false
	}
	m := mt.New().Interface()

	// Unmarshal bytes into embedded message.
	fdValue := fds.ByNumber(genid.Any_Value_field_number)
	value := msg.Get(fdValue)
	err = proto.UnmarshalOptions{
		AllowPartial: true,
		Resolver:     e.opts.Resolver,
	}.Unmarshal(value.Bytes(), m)
	if err != nil {
		return false
	}

	// Get current encoder position. If marshaling fails, reset encoder output
	// back to this position.
	pos := e.Snapshot()

	// Field name is the proto field name enclosed in [].
	e.WriteName("[" + typeURL + "]")
	err = e.marshalMessage(m.ProtoReflect(), true)
	if err != nil {
		e.Reset(pos)
		return false
	}
	return true
}

func (e *Encoder) marshalMessage(m protoreflect.Message, inclDelims bool) error {
	if e.override.HasMarshalMessage {
		return e.delegate.MarshalMessage(m, inclDelims)
	}
	return e.MarshalMessage(m, inclDelims)
}

func (e *Encoder) marshalField(name string, val protoreflect.Value, fd protoreflect.FieldDescriptor) error {
	if e.override.HasMarshalField {
		return e.delegate.MarshalField(name, val, fd)
	}
	return e.MarshalField(name, val, fd)
}

func (e *Encoder) marshalSingular(val protoreflect.Value, fd protoreflect.FieldDescriptor) error {
	if e.override.HasMarshalSingular {
		return e.delegate.MarshalSingular(val, fd)
	}
	return e.MarshalSingular(val, fd)
}

func (e *Encoder) marshalList(name string, list protoreflect.List, fd protoreflect.FieldDescriptor) error {
	if e.override.HasMarshalList {
		return e.delegate.MarshalList(name, list, fd)
	}
	return e.MarshalList(name, list, fd)
}

func (e *Encoder) marshalMap(name string, mmap protoreflect.Map, fd protoreflect.FieldDescriptor) error {
	if e.override.HasMarshalMap {
		return e.delegate.MarshalMap(name, mmap, fd)
	}
	return e.MarshalMap(name, mmap, fd)
}

func (e *Encoder) marshalUnknown(b []byte) {
	if e.override.HasMarshalUnknown {
		e.delegate.MarshalUnknown(b)
	}
	e.MarshalUnknown(b)
}

func (e *Encoder) marshalAny(m protoreflect.Message) bool {
	if e.override.HasMarshalAny {
		return e.delegate.MarshalAny(m)
	}
	return e.MarshalAny(m)
}
