# --------------------------------------------------------------------- build ---

FROM alpine:3.20 AS build

WORKDIR /opt/app
COPY . .
# simulate a compile/build step that produces output artifacts.
RUN mkdir -p dist \
  && echo "built at fixed content" > dist/app.txt \
  && echo "console.log('hello')" > dist/index.js

# ------------------------------------------------------------------- package ---

FROM alpine:3.20 AS package

RUN apk add --no-cache zip
COPY --from=build /opt/app/dist /opt/app/dist
RUN cd /opt/app/dist && zip -r -X /tmp/package.zip .
