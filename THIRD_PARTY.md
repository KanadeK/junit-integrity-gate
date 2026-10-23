# Third-party material and dependency scope

Original source and original synthetic fixtures are MIT licensed by KanadeK.
The application imports only the Go standard library. There are no external
Go modules, vendored XML libraries, copied competitor code, fonts or media.

Release binaries statically include Go 1.27.3 runtime/standard-library code.
Its BSD notice is preserved verbatim in `third_party/Go-LICENSE.txt`, included
in every binary archive, along with Go's additional patent grant. This notice
is separate from the project's MIT license.
Later rebuilds must retain the applicable Go notices.

Developer-only Python acceptance requirements generate real producer reports:
pytest (MIT), unittest-xml-reporting (BSD-2-Clause), and their resolved development
dependencies, including lxml (see that package's own multi-library notices).
They are installed normally for tests, not vendored or distributed in the
application archives. govulncheck is a development audit tool under Go's BSD
license and is likewise not shipped in the program. Build tools and CI actions
have their own upstream licenses. A clean runtime module graph does not audit
the toolchain, CI service, operating system or all development tools.
