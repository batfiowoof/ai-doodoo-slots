import ShopScreen from "@/components/ShopScreen";

// THE VAULT — cosmetics shop. The screen pulls the catalog, the caller's
// inventory, and the equipped loadout from the api; nothing here decides
// ownership or price.
export default function ShopPage() {
  return <ShopScreen />;
}
