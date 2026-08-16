# ssh-hosts

OpenSSHのクライアント設定から、具体的なホスト名だけを一覧表示する小さなCLIです。
`Include` で分割された設定も再帰的に読み込みます。

## インストール

```console
go install github.com/zaubermaerchen/ssh-hosts@latest
```

または、このリポジトリでビルドします。

```console
go build .
```

## 使い方

引数を省略すると `~/.ssh/config` を読み込みます。

```console
$ ssh-hosts
production
staging
development
```

別の設定ファイルを起点にすることもできます。

```console
$ ssh-hosts ./testdata/ssh_config
```

次のような設定では `production` と `staging` だけが表示されます。

```sshconfig
Host production staging *.example.com host? !excluded
Include conf.d/*.conf
```

## 挙動

- `*`、`?`、`[` を含むHostパターンと、`!` から始まる否定パターンを除外します。
- 同じホスト名は最初の1回だけ、設定に現れた順で表示します。
- `Host` / `Match` ブロック内を含むすべての `Include` を探索します。
- Includeの複数パス、glob、`${VAR}`、`~`、OpenSSHの静的な `%` トークンを扱います。
- ユーザー設定の相対Includeパスは、OpenSSHと同じく `~/.ssh` を基準にします。
- globに一致するファイルがない場合は無視します。明示したファイルを読めない場合はエラーになります。
- 接続先がなければ展開できない `%h` などのトークンはエラーになります。

このツールは設定に記述された候補を列挙するため、`Match` 条件や接続時の適用可否は評価しません。
