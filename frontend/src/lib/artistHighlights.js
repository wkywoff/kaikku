export function countLiked(likedIds, ...idLists) {
  return new Set(idLists.flat().filter((id) => likedIds.has(id))).size;
}
