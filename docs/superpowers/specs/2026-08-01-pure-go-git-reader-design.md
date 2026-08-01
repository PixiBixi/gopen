# Lecteur `.git` pur Go avec repli sur `git`

Date: 2026-08-01
Statut: design validé, prêt pour plan d'implémentation

## Problème

`gopen` invoque `git` en subprocess quatre fois par exécution (`git.go:104-138`):
`rev-parse --git-dir`, `remote get-url <name>`, `rev-parse --abbrev-ref HEAD`,
`rev-parse --show-toplevel`.

Mesures réalisées sur la machine de développement (macOS 25.5, git 2.54.0, Go 1.26):

| Métrique | Valeur |
|---|---|
| Binaire `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"` | 2 148 274 o |
| Coût moyen d'un fork `git` | ~16,5 ms |
| Coût cumulé des 4 appels | ~66 ms |
| End-to-end `gopen -c README.md` | ~102 ms |

Les données nécessaires (racine du dépôt, branche courante, URL du remote) tiennent
dans deux petits fichiers, `.git/HEAD` et `.git/config`. Les lire directement coûte
quelques dizaines de microsecondes.

## Objectifs et non-objectifs

Objectifs:

- Supprimer les forks `git` du chemin nominal.
- Ne jamais produire une URL différente de celle que `git` aurait donnée, y compris
  silencieusement.
- Rester sans dépendance externe (stdlib uniquement).

Non-objectifs:

- Réduire la taille du binaire. `os/exec` reste linké pour `openBrowser()` et
  `copyToClipboard()` (`output.go:22-70`), donc aucun paquet ne disparaît. Le nouveau
  code ajoutera quelques kilo-octets. La cible est "taille quasi inchangée".
- Réimplémenter `git`. Tout cas ambigu est délégué au binaire `git`.
- Adopter `go-git` ou toute autre bibliothèque. Le poids et l'arbre de dépendances
  sont incompatibles avec un CLI zéro-dépendance.

## Architecture

Nouveau fichier `gitfile.go` (plus `gitfile_test.go`) portant le lecteur pur Go.
`git.go` conserve les helpers subprocess existants et devient la couche de repli.
`getRepoContext()` reste le point d'entrée unique et orchestre les deux chemins:

```text
getRepoContext(targetPath, remoteName)
  │
  ├─ needsGitFallback()  →  oui  ──►  chemin subprocess (4 appels git)
  │
  ├─ readRepoContextFromDisk()  ──►  succès  ──►  retour
  │
  └─ échec de parsing  ────────────►  chemin subprocess (dernier filet)
```

Invariant central: soit le chemin rapide est certain du résultat, soit `git` tranche.
Aucune divergence silencieuse n'est possible.

### Composants de `gitfile.go`

#### `discoverGitDir(start string) (gitDir, commonDir, workTree string, err error)`

Remonte l'arborescence depuis `start` jusqu'à trouver une entrée `.git`, en s'arrêtant
à la racine du système de fichiers.

- `.git` est un dossier: `gitDir` = ce dossier, `commonDir` = `gitDir`,
  `workTree` = son parent.
- `.git` est un fichier: il contient `gitdir: <path>` (worktrees liés et submodules).
  Le chemin, s'il est relatif, se résout depuis le dossier contenant le fichier `.git`.
  Si `<gitDir>/commondir` existe, il donne `commonDir` (résolu relativement à `gitDir`);
  sinon `commonDir` = `gitDir`.

`commonDir` importe car dans un worktree lié, `HEAD` vit dans `gitDir` mais `config`
vit dans `commonDir`.

#### `parseGitConfig(r io.Reader) ([]configEntry, error)`

Parseur INI minimal. Il retourne une **liste ordonnée** d'entrées `{key, value}` avec
des clés aplaties façon git (`remote.origin.url`, `core.bare`), et non une map, parce
que l'ordre est significatif (voir la règle multi-url ci-dessous).

Doit gérer: sections `[section]`, sous-sections entre guillemets
`[remote "origin"]`, forme courte `[section.subsection]`, commentaires `#` et `;`,
espaces autour du `=`, valeurs entre guillemets avec échappements, continuation de
ligne par `\`, et clé sans valeur (booléen implicite à `true`).

Les noms de section et de clé sont insensibles à la casse; les noms de sous-section
sont sensibles à la casse. C'est la règle de git.

#### `remoteURLFromConfig(entries []configEntry, remoteName string) (string, bool)`

Retourne la **première** valeur `remote.<name>.url` dans l'ordre du fichier.

Ce point a été vérifié expérimentalement et constitue un piège réel:

```text
[remote "origin"]
    url = https://first.example/repo.git
    url = https://second.example/repo.git
    url = https://third.example/repo.git
```

| Commande | Résultat |
|---|---|
| `git remote get-url origin` | `https://first.example/repo.git` |
| `git config --get remote.origin.url` | `https://third.example/repo.git` |

`gopen` utilise aujourd'hui `git remote get-url`, donc la sémantique à reproduire est
"première valeur gagne". Un parseur écrit naturellement applique "dernière valeur
gagne" et divergerait. La règle s'applique dans l'ordre du fichier même si plusieurs
sections `[remote "origin"]` coexistent, git les concaténant.

#### `branchFromHEAD(gitDir string) (string, error)`

Lit `<gitDir>/HEAD`.

- `ref: refs/heads/<name>` retourne `<name>`. Seul le préfixe `refs/heads/` est retiré,
  pour qu'une branche `feature/foo` reste intacte.
- Contenu correspondant à un SHA brut (HEAD détaché) retourne `"HEAD"`, ce que renvoie
  déjà `git rev-parse --abbrev-ref HEAD`.
- Toute autre forme retourne une erreur, ce qui déclenche le repli.

#### `needsGitFallback() bool`

Détection préventive des situations où le chemin rapide serait faux. Retourne `true` si:

- `GIT_DIR` ou `GIT_WORK_TREE` est défini dans l'environnement.
- Un fichier de configuration dans la portée contient `insteadof`, `[include` ou
  `includeif` (recherche de sous-chaîne insensible à la casse sur les octets bruts,
  sans parsing).

Fichiers scannés, dans l'ordre de portée de git:

| Portée | Emplacement |
|---|---|
| Système | `$GIT_CONFIG_SYSTEM` si défini, sinon `/etc/gitconfig`. Ignoré si `GIT_CONFIG_NOSYSTEM` est défini. |
| Global | `$GIT_CONFIG_GLOBAL` si défini, sinon `$XDG_CONFIG_HOME/git/config` puis `~/.gitconfig` |
| Local | `<commonDir>/config` |

`url.<base>.insteadOf` réécrit l'URL du remote et est courant en environnement
d'entreprise. Le manquer produirait une URL fausse sans avertissement, ce que
l'invariant interdit. Le coût du scan est de quelques lectures de petits fichiers,
négligeable devant les 16 ms d'un fork.

### Correction collatérale: cohérence des symlinks

`getRepoRoot()` retourne aujourd'hui le chemin résolu par `git`, symlinks déréférencés,
alors que `targetPath` issu de `resolvePath()` (`git.go:33-56`) ne l'est pas. Sur macOS
cela produit `filepath.Rel("/private/var/...", "/var/...")` et un `relPath` truffé de
`../`. C'est précisément le contournement `realPath()` que portent les tests
(`git_test.go:204-211`).

Le chemin rapide remonte depuis `targetPath`, donc `repoRoot` et `targetPath` vivent
dans le même espace de noms et `relPath` est correct par construction.

Pour que les deux chemins de code se comportent identiquement, la branche de repli
applique `filepath.EvalSymlinks` à `targetPath` avant le calcul de `filepath.Rel`.
Si `EvalSymlinks` échoue, on conserve `targetPath` tel quel plutôt que d'échouer.

## Nouveau flag `--print`

Ajout d'un mode de sortie sans effet de bord, utile pour scripter `gopen` et
indispensable pour mesurer proprement le end-to-end (les modes existants forkent
`open` ou `pbcopy`, ce qui masque le gain).

- Formes: `-p`, `--print`. `-p` est libre (`-v`, `-c`, `-r`, `-l` sont pris).
- Champ `print bool` dans `config` (`args.go:9-17`), casse `case "-p", "--print":`
  dans `parseArgs()`.
- Comportement: écrit l'URL nue sur stdout suivie d'un saut de ligne, rien d'autre,
  et sort en 0. Contrairement aux modes existants il n'y a pas de préfixe
  (`Opening:` ou `URL copied to clipboard:`), pour rester utilisable dans un pipe.
- Précédence entre modes de sortie mutuellement exclusifs: `print` > `copy` > `open`.
  Combiner `-p` et `-c` n'est pas une erreur, `-p` gagne.

À mettre à jour en conséquence: `usage()` dans `args.go:19-41`, les trois scripts de
complétion dans `completion.go`, et le README.

## Tests

Le filet principal est un **test différentiel**: pour chaque fixture, le résultat de
`readRepoContextFromDisk()` doit être strictement égal à celui du chemin subprocess.
C'est ce test qui garantit l'invariant, pas les assertions sur des valeurs codées en dur.

Fixtures à construire avec de vrais appels `git`, dans la lignée de `newTmpGitRepo()`
(`git_test.go:111-127`):

| Fixture | Ce qu'elle couvre |
|---|---|
| Repo simple, remote HTTPS | Cas nominal |
| Repo simple, remote SSH | `convertToHTTPS()` en aval |
| Cible en sous-répertoire | Calcul de `relPath` |
| Branche `feature/foo` | Retrait du seul préfixe `refs/heads/` |
| HEAD détaché | Retour `"HEAD"` |
| Worktree lié (`git worktree add`) | `.git` fichier, `commondir`, HEAD hors du commonDir |
| Submodule | `.git` fichier, `gitdir:` relatif |
| Remote multi-url | Règle "première valeur gagne" |
| Remote inexistant | Propagation de l'erreur |
| Hors dépôt | Erreur "not in a git repository" |
| `GIT_DIR` défini | Déclenchement du repli |
| `insteadOf` en config globale | Déclenchement du repli, via `GIT_CONFIG_GLOBAL` pointant sur une fixture |

Tests unitaires table-driven, avec fixtures inline, sur `parseGitConfig()` et
`branchFromHEAD()`, incluant les formes malformées qui doivent produire une erreur.

Contrainte d'isolation: les tests qui manipulent la configuration globale doivent
utiliser `t.Setenv` avec `GIT_CONFIG_GLOBAL`, `GIT_CONFIG_SYSTEM` et
`GIT_CONFIG_NOSYSTEM` pour ne jamais dépendre de la configuration de la machine ni
de celle du runner CI.

## Protocole de mesure

Les quatre mesures sont à produire avant et après, et à consigner dans le rapport final.

1. **Micro-benchmark Go.** `BenchmarkGetRepoContext` avec deux variantes, pur Go et
   subprocess, sur un dépôt temporaire. Isole exactement ce qui change.
2. **End-to-end `hyperfine`.** `hyperfine 'gopen --print README.md'` sur l'ancien et le
   nouveau binaire, avec warmup. `--print` élimine le bruit de `pbcopy` et de `open`.
3. **Taille du binaire.** `wc -c` avant et après, plus un diff de
   `go tool nm -size -sort size` pour attribuer précisément les octets ajoutés.
4. **Non-régression sur cas réels.** Sur ce dépôt et un worktree créé pour l'occasion,
   comparer la sortie de `gopen --print` entre l'ancien et le nouveau binaire.

Baseline déjà relevée: binaire 2 148 274 o, end-to-end ~102 ms avec `-c`.
La valeur end-to-end de référence avec `--print` sera à établir, le flag n'existant pas
encore sur la version actuelle. Pour disposer d'un point de comparaison honnête,
`--print` doit être implémenté et mesuré **avant** le remplacement du chemin git,
en deux étapes distinctes.

## Ordre d'implémentation

L'ordre est contraint par le protocole de mesure.

1. Ajouter `--print` seul, sur le chemin git actuel. Mesurer la baseline complète
   (benchmark, hyperfine, taille) avec ce binaire.
2. Ajouter `gitfile.go` et sa suite de tests, chemin rapide non branché.
3. Brancher `getRepoContext()` sur le chemin rapide avec repli, appliquer la correction
   `EvalSymlinks` sur la branche de repli.
4. Reprendre les quatre mesures et produire le comparatif.
5. Mettre à jour le README et les scripts de complétion.

Chaque étape est un commit séparé, conventional commits, un scope par commit:
`feat(args)`, `feat(git)`, `refactor(git)`, `docs`.

## Risques

| Risque | Mitigation |
|---|---|
| Le parseur INI diverge de git sur une forme exotique | Test différentiel systématique; tout échec de parsing déclenche le repli plutôt qu'une erreur |
| Un mécanisme de config non anticipé change l'URL | Le scan `insteadOf`/`include`/`includeIf` couvre les vecteurs connus; un vecteur inconnu resterait invisible, à accepter |
| Le gain perf est masqué par le lancement du process Go | Attendu autour de 3 à 5 ms incompressibles; le benchmark Go isole la part réellement optimisée |
| Le binaire grossit plus que prévu | Le diff `go tool nm` identifie la cause; seuil d'alerte fixé à +50 Ko |
