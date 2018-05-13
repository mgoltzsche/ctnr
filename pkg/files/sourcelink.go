package files

type sourceLink struct {
	Source
}

func NewSourceLink(s Source) Source {
	return &sourceLink{s}
}

func (s sourceLink) Type() SourceType {
	return TypeLink
}
