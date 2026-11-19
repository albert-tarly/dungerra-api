# Playing the Game

Distances below are in game **units**. For scale, your character is 24 units wide.

## Movement

Dungerra uses rotate-and-move controls, like driving a tank:

| Action | Effect |
| --- | --- |
| **Rotate left / right** | Turn on the spot at 180° per second (a full turn takes 2 seconds) |
| **Forward** | Move in the direction you are facing at 300 units/second |
| **Backward** | Move away from the direction you are facing at the same speed |
| **Shoot** | Fire the selected weapon. Hold it to keep firing as fast as the weapon allows. |

Walls, living enemies and other players block you. If you run into a wall at an angle, you slide along it instead of stopping dead, which makes moving through corridors easier.

The server runs the game at 30 updates per second and makes all the decisions about movement, hits and damage.

## Health

- You have **6 lives** (hit points). Most hits take 1 life. Rockets and some weapons take more, and shotgun pellets take a fraction of a life.
- After you take a hit and survive, you are **invulnerable for 1 second**. Nothing can hurt you during that time, not even explosions.
- Use an **aid kit** to get back 1 life, up to the maximum of 6. See [Items, Loot and Shops](items-and-shops.md).

## Light and vision

The world is dark. What you can see depends on light.

### Your torch

You always carry a torch that lights a circle **200 units** around you. Inside it you can see:

- enemies,
- bullets and rockets,
- items lying on the ground,
- shops.

**Walls** are different: you see their outlines up to **1,500 units** away, so you can always read the layout around you. If a guard is visible, you also see the wall it patrols.

### Other players' torches

Light is shared. If another player with a lit torch is within 1,500 units of you, you see them, and you also see whatever their torch lights up. Exploring with friends really does light up more of the map.

The game also shares the positions of all players who have a lit torch with everyone in the session, so your client may show them on a map or radar.

### Night-vision goggles

Goggles change the rules for **20 seconds**:

- You see everything within **1,500 units**: enemies, items, shops and bullets, even in the dark.
- **Your torch goes out.** Other players can't see you and your position is no longer shared, so you are hidden from them.
- Enemies have to get much closer to notice you (see [Enemies](enemies.md#how-enemies-notice-you)).

Each extra pair of goggles you use adds another 20 seconds.

### Things you can always see

- **Sniper laser sights.** A red laser is visible from up to 1,500 units away, even in total darkness. If you see one pointing at you, move.
- **Rocket explosions** are visible from far away.
- **Railgun shots** are visible to anyone within 1,500 units of the beam.

## The world

The world is endless and generated in square **areas** 2,000 units across. New areas are created as players get close to them. Every area contains:

- **20–40 wall segments**, each 200–300 units long, forming the corridors and cover,
- **a shop** in the centre of the area,
- **one enemy tower** somewhere in the area,
- **guards**: almost every wall (90%) has a soldier, lieutenant or sniper posted next to it.

Enemies are only active while a player is within 1,500 units of them. **Killed enemies don't come back**, so a cleared area stays cleared.

## Player vs player

All players in a session can damage each other with any weapon. Killing another player earns you **100 money** and a kill, and they drop a chest with part of their belongings (see [Dying](#dying)).

## Dying

When your lives reach 0:

1. You drop a **chest** where you fell. It holds a random **⅓ to all** of your money, and the same random share of each item you carried, including weapons and ammo. The blaster is never dropped.
2. Your run ends and your score is sent to the [leaderboard](scoring.md).
3. You can **respawn** whenever you choose.

Anyone can pick up the chest, including you. It disappears after **5 minutes**. You can always see your own chest when you are in the same part of the world, even in the dark, so you can find your way back to it.

## Respawning

When you respawn:

- you appear at the centre of an area next to the one you died in (where that area's shop is),
- you start fresh: 6 lives, a blaster with 6 shots, **no money, no score, no kills, no other items**,
- you are **invulnerable for 3 seconds**.

Each life is a separate run with its own score.
