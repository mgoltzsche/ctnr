FROM alpine:3.7
#as first
ARG TESTARG1
ARG TESTARG2=argval
LABEL maintainer Max Goltzsche "max.goltzsche@gmail.com"
LABEL com.github.mgoltzsche.ctnr.test.label1="test value1"
LABEL "com.github.mgoltzsche.ctnr.test.label2"="test value2"
ENV CONFIGX configxval
ENV CFG1=val1 CFG2="$TESTARG2 suffix"
RUN set -x \
	&& apk add --update --no-cache nginx
COPY --chown=root:root entrypoint.sh *.conf /
EXPOSE 80 443/tcp
ENTRYPOINT echo hello
ENTRYPOINT [ "/entrypoint.sh" ]

FROM alpine:3.7
ENTRYPOINT [ "/usr/bin/nginx", "-c" ]
STOPSIGNAL 9