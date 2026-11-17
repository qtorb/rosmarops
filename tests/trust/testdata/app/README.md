# The app's copy of space-view.v1

`space-view.v1.schema.json` is copied byte for byte from qtorb/rosmarops-app at
commit `c411b3de4b7f6edebd1daa9b0018e47fa0a6f6a4`, file
`tests/fixtures/space-view.v1.schema.json`. The app's `public/space-view.mjs`
`SCHEMA` is the same document without its `$schema`.

`TestAppSpaceViewContract` validates the program's space-view packages against
it, with no network. When the app changes its copy, replace this file from the
new commit and update the commit above.
