git-hooks
=========

Collection of git hooks I consider useful. Most of the pre-commit hooks are Go specific.


usage
-----
* add them as submodule via

	git submodule add https://github.com/nightlyone/git-hooks git-hooks

* enable commit hooks via

        cd .git ; rm -rf hooks; ln -s ../git-hooks hooks ; cd ..

contribution
------------
Pull requests welcome!
