# knowledge/integrations/

One folder per third-party system we integrate with. Our distilled notes live at the root; raw scraped/vendor material lives under `vendor/`.

Recommended shape:

```
<vendor>/
  README.md        what it is, where it's used in our app, auth, base URLs
  endpoints.md     the endpoints we actually call
  learnings.md     quirks discovered the hard way
  vendor/          raw docs, postman collections, OpenAPI specs
```
